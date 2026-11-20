//go:build e2e

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/extract"
	"github.com/Amrit-Code/upstreamwatch/internal/extract/unity"
	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/ingest"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

// advisoryParityFiles names, per fixture in parityFixtures, the HEAD lockfile
// whose advisory ids are compared with osv-scanner's.
var advisoryParityFiles = map[string]string{
	"glow": "go.mod",
	"cleo": "poetry.lock",
}

// sideOSVScanner names osv-scanner as the side a differing advisory id
// appears on.
const sideOSVScanner paritySide = "osv-scanner"

// advisoryException excuses one advisory id of one fixture's file that
// appears only on Side. Reason names the decision in docs/decisions.md
// ("Lead decisions for M2") that explains the difference.
type advisoryException struct {
	Fixture    string
	File       string
	AdvisoryID string
	Side       paritySide
	Reason     string
}

// advisoryParityExceptions are the differences a recorded decision explains.
// Each must match a difference of its fixture and file, so a stale entry
// fails the test.
var advisoryParityExceptions = []advisoryException{}

// osvScannerOutput is the part of osv-scanner's JSON output the comparison
// reads.
type osvScannerOutput struct {
	Results []struct {
		Packages []struct {
			Vulnerabilities []struct {
				ID string `json:"id"`
			} `json:"vulnerabilities"`
			Groups []struct {
				IDs []string `json:"ids"`
			} `json:"groups"`
		} `json:"packages"`
	} `json:"results"`
}

// osvScannerIDs is osv-scanner's side of one file: every vulnerability id it
// reported and its alias groups.
type osvScannerIDs struct {
	IDs    []string
	Groups [][]string
}

// decodeOSVScanner returns every vulnerability id and alias group in
// osv-scanner's JSON output, over all results and packages.
func decodeOSVScanner(data []byte) (osvScannerIDs, error) {
	var out osvScannerOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return osvScannerIDs{}, fmt.Errorf("decode osv-scanner output: %w", err)
	}
	var s osvScannerIDs
	for _, r := range out.Results {
		for _, p := range r.Packages {
			for _, v := range p.Vulnerabilities {
				s.IDs = append(s.IDs, v.ID)
			}
			for _, g := range p.Groups {
				s.Groups = append(s.Groups, slices.Clone(g.IDs))
			}
		}
	}
	slices.Sort(s.IDs)
	s.IDs = slices.Compact(s.IDs)
	return s, nil
}

// openAdvisoryIDs returns, per manifest path, the sorted ids of the
// advisories with an open match (ExposedTo zero) on an interval of that
// manifest, over the exposures of details.
func openAdvisoryIDs(details []model.FindingDetail) map[string][]string {
	sets := map[string]map[string]bool{}
	for _, d := range details {
		for _, e := range d.Exposures {
			if !e.Match.IsOpen() {
				continue
			}
			path := e.Interval.ManifestPath
			if sets[path] == nil {
				sets[path] = map[string]bool{}
			}
			sets[path][e.Match.AdvisoryID] = true
		}
	}
	out := make(map[string][]string, len(sets))
	for path, ids := range sets {
		out[path] = slices.Sorted(maps.Keys(ids))
	}
	return out
}

// advisoryResult is the outcome of comparing one file's advisory ids.
type advisoryResult struct {
	fixture, file string
	ours, theirs  []string
	// onlyUpw and onlyOSV are the differing ids no exception excuses,
	// sorted. An osv-scanner alias group our side has no id of contributes
	// every id in it.
	onlyUpw, onlyOSV []string
	// excused counts the differing ids exceptions excused.
	excused int
	// unused are the file's exceptions that matched no difference.
	unused []advisoryException
}

func (r advisoryResult) ok() bool {
	return len(r.onlyUpw) == 0 && len(r.onlyOSV) == 0 && len(r.unused) == 0
}

// compareAdvisories compares our ids with osv-scanner's as sets, treating
// ids osv-scanner groups as aliases as one advisory that our side has when
// it has any of them, and excuses the differences that the exceptions of
// fixture and file match. An id osv-scanner reports outside every group is
// a group of its own.
func compareAdvisories(fixture, file string, ours []string, theirs osvScannerIDs, exceptions []advisoryException) advisoryResult {
	r := advisoryResult{fixture: fixture, file: file, ours: slices.Clone(ours), theirs: slices.Clone(theirs.IDs)}
	slices.Sort(r.ours)
	r.ours = slices.Compact(r.ours)

	groups := slices.Clone(theirs.Groups)
	grouped := map[string]bool{}
	for _, g := range groups {
		for _, id := range g {
			grouped[id] = true
		}
	}
	for _, id := range theirs.IDs {
		if !grouped[id] {
			groups = append(groups, []string{id})
			grouped[id] = true
		}
	}

	var fileExceptions []advisoryException
	for _, x := range exceptions {
		if x.Fixture == fixture && x.File == file {
			fileExceptions = append(fileExceptions, x)
		}
	}
	used := make([]bool, len(fileExceptions))
	excuse := func(side paritySide, id string) bool {
		for i, x := range fileExceptions {
			if x.Side == side && x.AdvisoryID == id {
				used[i] = true
				return true
			}
		}
		return false
	}

	onlyOSV := map[string]bool{}
	for _, g := range groups {
		if slices.ContainsFunc(g, func(id string) bool { return slices.Contains(r.ours, id) }) {
			continue
		}
		for _, id := range g {
			onlyOSV[id] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(onlyOSV)) {
		if excuse(sideOSVScanner, id) {
			r.excused++
			continue
		}
		r.onlyOSV = append(r.onlyOSV, id)
	}
	for _, id := range r.ours {
		if grouped[id] {
			continue
		}
		if excuse(sideUpw, id) {
			r.excused++
			continue
		}
		r.onlyUpw = append(r.onlyUpw, id)
	}
	for i, x := range fileExceptions {
		if !used[i] {
			r.unused = append(r.unused, x)
		}
	}
	return r
}

// report describes a comparison: both sets, the differences, sorted, and
// unused exceptions.
func (r advisoryResult) report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: upw %d ids, osv-scanner %d ids; %d differences excused\n",
		r.fixture, r.file, len(r.ours), len(r.theirs), r.excused)
	list := func(name string, ids []string) {
		fmt.Fprintf(&b, "%s (%d): %s\n", name, len(ids), strings.Join(ids, " "))
	}
	list(string(sideUpw), r.ours)
	list(string(sideOSVScanner), r.theirs)
	list("only in "+string(sideUpw), r.onlyUpw)
	list("only in "+string(sideOSVScanner), r.onlyOSV)
	for _, x := range r.unused {
		fmt.Fprintf(&b, "exception matches no difference: %+v\n", x)
	}
	return b.String()
}

// findOSVScanner returns the osv-scanner binary: the one on PATH, else the
// one go install put in the first GOPATH entry's bin directory.
func findOSVScanner() (string, error) {
	if p, err := exec.LookPath("osv-scanner"); err == nil {
		return p, nil
	}
	out, err := exec.Command("go", "env", "GOPATH").Output()
	if err != nil {
		return "", fmt.Errorf("osv-scanner not on PATH and go env GOPATH failed: %w", err)
	}
	dirs := filepath.SplitList(strings.TrimSpace(string(out)))
	if len(dirs) == 0 {
		return "", errors.New("osv-scanner not on PATH and GOPATH is empty")
	}
	p, err := exec.LookPath(filepath.Join(dirs[0], "bin", "osv-scanner"))
	if err != nil {
		return "", fmt.Errorf("osv-scanner not on PATH or in GOPATH/bin: %w", err)
	}
	return p, nil
}

func TestAdvisoryParity(t *testing.T) {
	if _, err := exec.LookPath("git-pkgs"); err != nil {
		t.Skipf("git-pkgs not on PATH, advisory parity not checked: %v", err)
	}
	scanner, err := findOSVScanner()
	if err != nil {
		t.Skipf("advisory parity not checked: %v", err)
	}
	for _, fx := range parityFixtures {
		t.Run(fx.slug, func(t *testing.T) {
			file, ok := advisoryParityFiles[fx.slug]
			if !ok {
				t.Fatalf("fixture %s has no lockfile in advisoryParityFiles", fx.slug)
			}
			env := parityEnv(t)
			clone := filepath.Join(t.TempDir(), fx.slug)
			runTool(t, env, "", "git", "clone", "--quiet", "--", fx.url, clone)
			runTool(t, env, clone, "git", "reset", "--hard", "--quiet", fx.commit)

			logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelWarn}))
			db := indexForAdvisories(t, logger, fx.slug, clone)

			hc := httpx.New(db, httpx.Options{}, logger)
			svc := vuln.NewService(osv.New(hc, osv.Options{}, logger), nil, nil, db, db, db, vuln.Options{}, logger)
			stats, err := svc.Run(t.Context(), model.Scope{})
			if err != nil {
				t.Fatalf("vulns: %v", err)
			}
			t.Logf("%s: vulns stats %v", fx.slug, stats)

			byPath := openAdvisoryIDsFromStore(t, db)
			pinned := pinnedOpenIntervals(t, db, file)
			if pinned < minHeadDeps {
				t.Errorf("too few pinned open intervals on %s to compare: %d, want at least %d", file, pinned, minHeadDeps)
			}
			for _, path := range slices.Sorted(maps.Keys(byPath)) {
				if path != file {
					t.Logf("%s: %s, not compared: %d open advisory ids: %s", fx.slug, path, len(byPath[path]), strings.Join(byPath[path], " "))
				}
			}

			theirs, err := decodeOSVScanner(runOSVScanner(t, env, scanner, clone, filepath.Join(clone, file)))
			if err != nil {
				t.Fatal(err)
			}
			r := compareAdvisories(fx.slug, file, byPath[file], theirs, advisoryParityExceptions)
			// Both fixtures have known open advisories at their pins, so an
			// empty side means a source outage or a stale pin, not parity.
			if len(r.ours) == 0 || len(r.theirs) == 0 {
				t.Errorf("no advisory ids to compare on %s: upw %d, osv-scanner %d, want at least one on each side (OSV unreachable, or the fixture pin no longer has open advisories)\n%s",
					file, len(r.ours), len(r.theirs), r.report())
			}
			if !r.ok() {
				t.Errorf("open advisory matches differ from osv-scanner\n%s", r.report())
			} else {
				t.Logf("%s", r.report())
			}
		})
	}
}

// indexForAdvisories syncs the clone at dir through its file:// URL with the
// ingest job and builds its timeline with the timeline job, as indexWithUpw
// does for the timeline referee, and returns the store for the vulns job.
func indexForAdvisories(t *testing.T, logger *slog.Logger, slug, dir string) *store.DB {
	t.Helper()
	ctx := t.Context()

	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "upw.sqlite"), logger)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	git := gitsrc.New(gitsrc.Options{
		ReposDir:      t.TempDir(),
		HomeDir:       t.TempDir(),
		AllowFileURLs: true,
	}, logger)
	targets := []model.TargetRepo{{Slug: slug, URL: "file://" + filepath.ToSlash(dir)}}
	if _, err := ingest.NewService(git, db, ingest.Options{Targets: targets}, logger).Run(ctx, model.Scope{}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	ext := extract.New(extract.Options{DeriveToolchainFromImages: false}, logger)
	tl := timeline.NewService(git, db, db, []timeline.Extractor{ext}, unity.New(),
		timeline.Options{Targets: []timeline.TargetSettings{{Slug: slug}}}, logger)
	stats, err := tl.Run(ctx, model.Scope{})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	t.Logf("%s: timeline stats %v", slug, stats)
	return db
}

// openAdvisoryIDsFromStore reads every unresolved advisory finding's detail
// and returns openAdvisoryIDs over them.
func openAdvisoryIDsFromStore(t *testing.T, db *store.DB) map[string][]string {
	t.Helper()
	findings, err := db.ListUnresolvedFindings(t.Context(), model.KindAdvisory)
	if err != nil {
		t.Fatalf("list advisory findings: %v", err)
	}
	details := make([]model.FindingDetail, 0, len(findings))
	for _, f := range findings {
		d, err := db.GetFindingDetail(t.Context(), f.ID)
		if err != nil {
			t.Fatalf("get finding detail: %v", err)
		}
		details = append(details, d)
	}
	return openAdvisoryIDs(details)
}

// pinnedOpenIntervals counts the open intervals with a resolved version on
// manifest path, the ones the vulns job queries and matches.
func pinnedOpenIntervals(t *testing.T, db *store.DB, path string) int {
	t.Helper()
	all, err := db.ListAllIntervals(t.Context())
	if err != nil {
		t.Fatalf("list intervals: %v", err)
	}
	n := 0
	for _, ri := range all {
		if ri.Interval.ManifestPath == path && ri.Interval.IsOpen() && ri.Interval.Version != "" {
			n++
		}
	}
	return n
}

// osvScannerConfig is the configuration osv-scanner runs with, relative to
// this package's directory. It makes osv-scanner scan the Go version in
// go.mod as package stdlib, which upstreamwatch matches as
// pkg:golang/toolchain, and overrides any osv-scanner.toml in the fixture.
const osvScannerConfig = "testdata/advisory_parity/osv-scanner.toml"

// runOSVScanner runs osv-scanner with osvScannerConfig on lockfile in dir and
// returns its standard output. osv-scanner exits 1 when it found
// vulnerabilities; any other failure fails the test.
func runOSVScanner(t *testing.T, env []string, scanner, dir, lockfile string) []byte {
	t.Helper()
	config, err := filepath.Abs(osvScannerConfig)
	if err != nil {
		t.Fatalf("osv-scanner config path: %v", err)
	}
	args := []string{"scan", "source", "--format", "json", "--config", config, "-L", lockfile}
	cmd := exec.CommandContext(t.Context(), scanner, args...)
	cmd.Dir = dir
	// osv-scanner's Go call analysis downloads modules into the empty HOME's
	// module cache, read-only by default, which the test's temporary
	// directory cleanup could not remove.
	cmd.Env = append(slices.Clone(env), "GOFLAGS=-modcacherw")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
		err = nil
	}
	if err != nil {
		t.Fatalf("osv-scanner %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes()
}

func TestAdvisoryParityCompare(t *testing.T) {
	t.Parallel()

	theirs := osvScannerIDs{
		IDs:    []string{"GHSA-aaaa", "GHSA-bbbb", "GO-2026-0001", "PYSEC-2026-1"},
		Groups: [][]string{{"PYSEC-2026-1", "GHSA-aaaa"}, {"GHSA-bbbb"}},
	}
	tests := []struct {
		name        string
		ours        []string
		exceptions  []advisoryException
		wantOK      bool
		wantOnlyUpw []string
		wantOnlyOSV []string
		wantExcused int
		wantUnused  int
	}{
		{
			name:   "one alias of a group is enough",
			ours:   []string{"GHSA-aaaa", "GHSA-bbbb", "GO-2026-0001"},
			wantOK: true,
		},
		{
			name:   "every alias of a group on our side",
			ours:   []string{"GHSA-aaaa", "GHSA-bbbb", "GO-2026-0001", "PYSEC-2026-1"},
			wantOK: true,
		},
		{
			name:        "missing group lists all its ids; ungrouped id is its own group",
			ours:        []string{"GHSA-bbbb"},
			wantOnlyOSV: []string{"GHSA-aaaa", "GO-2026-0001", "PYSEC-2026-1"},
		},
		{
			name:        "id only on our side",
			ours:        []string{"GHSA-aaaa", "GHSA-bbbb", "GO-2026-0001", "GO-2026-9999"},
			wantOnlyUpw: []string{"GO-2026-9999"},
		},
		{
			name: "exceptions excuse their side, fixture and file only",
			ours: []string{"GHSA-aaaa", "GHSA-bbbb", "GO-2026-9999"},
			exceptions: []advisoryException{
				{Fixture: "fx", File: "go.mod", AdvisoryID: "GO-2026-9999", Side: sideUpw, Reason: "test"},
				{Fixture: "fx", File: "go.mod", AdvisoryID: "GO-2026-0001", Side: sideOSVScanner, Reason: "test"},
				{Fixture: "other", File: "go.mod", AdvisoryID: "GO-2026-0001", Side: sideOSVScanner, Reason: "test"},
				{Fixture: "fx", File: "poetry.lock", AdvisoryID: "GO-2026-0001", Side: sideOSVScanner, Reason: "test"},
			},
			wantOK:      true,
			wantExcused: 2,
		},
		{
			name: "exception on the wrong side does not excuse",
			ours: []string{"GHSA-aaaa", "GHSA-bbbb", "GO-2026-0001", "GO-2026-9999"},
			exceptions: []advisoryException{
				{Fixture: "fx", File: "go.mod", AdvisoryID: "GO-2026-9999", Side: sideOSVScanner, Reason: "test"},
			},
			wantOnlyUpw: []string{"GO-2026-9999"},
			wantUnused:  1,
		},
		{
			name: "stale exception fails",
			ours: []string{"GHSA-aaaa", "GHSA-bbbb", "GO-2026-0001"},
			exceptions: []advisoryException{
				{Fixture: "fx", File: "go.mod", AdvisoryID: "GO-2026-0002", Side: sideUpw, Reason: "test"},
			},
			wantUnused: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := compareAdvisories("fx", "go.mod", tt.ours, theirs, tt.exceptions)
			if got := r.ok(); got != tt.wantOK {
				t.Errorf("ok() = %v, want %v\n%s", got, tt.wantOK, r.report())
			}
			if !slices.Equal(r.onlyUpw, tt.wantOnlyUpw) {
				t.Errorf("onlyUpw = %v, want %v", r.onlyUpw, tt.wantOnlyUpw)
			}
			if !slices.Equal(r.onlyOSV, tt.wantOnlyOSV) {
				t.Errorf("onlyOSV = %v, want %v", r.onlyOSV, tt.wantOnlyOSV)
			}
			if r.excused != tt.wantExcused {
				t.Errorf("excused = %d, want %d", r.excused, tt.wantExcused)
			}
			if len(r.unused) != tt.wantUnused {
				t.Errorf("unused = %v, want %d entries", r.unused, tt.wantUnused)
			}
		})
	}
}

func TestAdvisoryParityDecodeOSVScanner(t *testing.T) {
	t.Parallel()

	data := []byte(`{"results":[{"source":{"path":"/x/poetry.lock","type":"lockfile"},"packages":[
		{"package":{"name":"urllib3","version":"2.5.0","ecosystem":"PyPI"},
		 "vulnerabilities":[{"id":"PYSEC-2026-1"},{"id":"GHSA-aaaa"}],
		 "groups":[{"ids":["PYSEC-2026-1","GHSA-aaaa"]}]},
		{"package":{"name":"stdlib","version":"1.26.0","ecosystem":"Go"},
		 "vulnerabilities":[{"id":"GO-2026-0001"},{"id":"GHSA-aaaa"}],
		 "groups":[{"ids":["GO-2026-0001"]}]}]}]}`)
	got, err := decodeOSVScanner(data)
	if err != nil {
		t.Fatalf("decodeOSVScanner: %v", err)
	}
	want := osvScannerIDs{
		IDs:    []string{"GHSA-aaaa", "GO-2026-0001", "PYSEC-2026-1"},
		Groups: [][]string{{"PYSEC-2026-1", "GHSA-aaaa"}, {"GO-2026-0001"}},
	}
	if !slices.Equal(got.IDs, want.IDs) {
		t.Errorf("IDs = %v, want %v", got.IDs, want.IDs)
	}
	if !slices.EqualFunc(got.Groups, want.Groups, slices.Equal) {
		t.Errorf("Groups = %v, want %v", got.Groups, want.Groups)
	}

	empty, err := decodeOSVScanner([]byte(`{"results":[]}`))
	if err != nil || len(empty.IDs) != 0 || len(empty.Groups) != 0 {
		t.Errorf("decodeOSVScanner(no results) = %v, %v, want no ids and no error", empty, err)
	}
	if _, err := decodeOSVScanner([]byte(`not json`)); err == nil {
		t.Errorf("decodeOSVScanner(not json) error = nil, want an error")
	}
}

func TestAdvisoryParityOpenIDs(t *testing.T) {
	t.Parallel()

	closedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	exposure := func(path, id string, open bool) model.Exposure {
		m := model.AdvisoryMatch{AdvisoryID: id}
		if !open {
			m.ExposedTo = closedAt
		}
		return model.Exposure{Match: m, Interval: model.DepInterval{ManifestPath: path}}
	}
	details := []model.FindingDetail{
		{Exposures: []model.Exposure{exposure("go.mod", "GO-2026-0002", true), exposure("go.mod", "GO-2026-0001", true)}},
		{Exposures: []model.Exposure{exposure("go.mod", "GO-2026-0002", true), exposure("go.mod", "GO-2026-0003", false)}},
		{Exposures: []model.Exposure{exposure("sub/go.mod", "GO-2026-0004", true)}},
		{Exposures: []model.Exposure{exposure("Dockerfile", "GO-2026-0005", false)}},
	}
	got := openAdvisoryIDs(details)
	want := map[string][]string{
		"go.mod":     {"GO-2026-0001", "GO-2026-0002"},
		"sub/go.mod": {"GO-2026-0004"},
	}
	if !maps.EqualFunc(got, want, slices.Equal) {
		t.Errorf("openAdvisoryIDs = %v, want %v", got, want)
	}
}
