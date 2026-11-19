package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// seededFindings are the IDs seedFindings stored.
type seededFindings struct {
	advisory, jobFailure int64
}

var (
	seedPkg = model.Package{PURL: "pkg:golang/github.com/example-org/lib-a", Ecosystem: "golang", Name: "github.com/example-org/lib-a"}
	seedDay = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
)

// seedFindings stores, in the database of the config at cfgPath, one target
// with one open interval of lib-a v1.0.0, an advisory matching it, an open
// advisory finding (score 7.5) linked to the interval, and an open
// job_failure finding for the ingest job (score 5).
func seedFindings(t *testing.T, cfgPath string) seededFindings {
	t.Helper()
	ctx := t.Context()
	a, err := openApp(ctx, cfgPath, slog.New(slog.DiscardHandler), noEnv)
	if err != nil {
		t.Fatalf("openApp() error = %v", err)
	}
	defer func() {
		if err := a.close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()
	db := a.db

	repo, err := db.UpsertRepo(ctx, "example-server", "https://github.com/example-org/example-server")
	if err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	const sha = "0000000000000000000000000000000000000001"
	if err := db.ApplyTimeline(ctx, model.TimelineUpdate{
		RepoID:    repo.ID,
		Commits:   []model.Commit{{SHA: sha, CommittedAt: seedDay, AuthorName: "Example Author", AuthorEmail: "author@example.com", Subject: "add lib-a", Paths: []string{"go.mod"}}},
		Manifests: map[string]model.ManifestInfo{"go.mod": {Ecosystem: "golang", Kind: model.ManifestKindManifest}},
		Intervals: []model.DepInterval{{ManifestPath: "go.mod", Package: seedPkg, Version: "v1.0.0", Requirement: "v1.0.0",
			Direct: model.DirectYes, FromSHA: sha, From: seedDay}},
		IndexedSHA: sha,
	}); err != nil {
		t.Fatalf("ApplyTimeline() error = %v", err)
	}
	ivs, err := db.ListAllIntervals(ctx)
	if err != nil || len(ivs) != 1 {
		t.Fatalf("ListAllIntervals() = %+v, %v; want one interval", ivs, err)
	}
	iv := ivs[0]

	const advID = "GO-2026-0001"
	if err := db.SaveAdvisory(ctx, model.Advisory{
		ID:        advID,
		Aliases:   []string{"CVE-2026-1001"},
		Summary:   "Example library mishandles input",
		Severity:  model.SeverityHigh,
		Published: seedDay,
		Modified:  seedDay,
		Affected: []model.AffectedPackage{{
			Package:  seedPkg,
			Ranges:   []model.AffectedRange{{Type: model.RangeSemver, Events: []model.RangeEvent{{Introduced: "0"}, {Fixed: "1.2.3"}}}},
			SourceDB: model.SourceDB(advID),
		}},
		Raw: []byte(`{"id":"` + advID + `"}`),
	}); err != nil {
		t.Fatalf("SaveAdvisory() error = %v", err)
	}
	if err := db.ReplaceMatches(ctx, repo.ID, []model.AdvisoryMatch{{
		RepoID: repo.ID, DepVersionID: iv.Interval.ID, AdvisoryID: advID, ExposedFrom: seedDay,
		FixedVersion: "v1.2.3", Confidence: model.ConfidenceHigh, ConfidenceReason: "go.mod pin",
	}}); err != nil {
		t.Fatalf("ReplaceMatches() error = %v", err)
	}

	adv, err := db.UpsertFinding(ctx, model.Finding{
		Kind: model.KindAdvisory, RepoID: repo.ID, PackageID: iv.PackageID, DepVersionID: iv.Interval.ID,
		AdvisoryID: advID, Fingerprint: "advisory|example-server|" + seedPkg.PURL + "|" + advID,
		Score: 7.5, RankReason: "high severity, exposed now", LastChangedAt: seedDay.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("UpsertFinding(advisory) error = %v", err)
	}
	job, err := db.UpsertFinding(ctx, model.Finding{
		Kind: model.KindJobFailure, Fingerprint: "job_failure|ingest",
		Score: 5, RankReason: "job failed", LastChangedAt: seedDay.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("UpsertFinding(job_failure) error = %v", err)
	}
	return seededFindings{advisory: adv.ID, jobFailure: job.ID}
}

// runFindings runs "upw findings" with args and returns the exit code, stdout
// and stderr.
func runFindings(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), append([]string{"findings"}, args...), &stdout, &stderr, noEnv)
	return code, stdout.String(), stderr.String()
}

func TestFindings_Table(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	seedFindings(t, cfg)
	code, stdout, stderr := runFindings(t, "-config", cfg)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr %q", code, stderr)
	}
	want := "" +
		"SCORE  KIND         REPO            PACKAGE                                  VERSION  ID            SEVERITY  CONFIDENCE  STATE\n" +
		"7.5    advisory     example-server  pkg:golang/github.com/example-org/lib-a  v1.0.0   GO-2026-0001  high      high        open\n" +
		"5.0    job_failure                                                                    ingest                              open\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestFindings_JSON(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "  base_url: https://upw.example.invalid/\n")
	ids := seedFindings(t, cfg)
	code, stdout, stderr := runFindings(t, "-json", "-config", cfg)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	want := []map[string]any{
		{
			"id": float64(ids.advisory), "kind": "advisory", "score": 7.5, "rank_reason": "high severity, exposed now",
			"repo": "example-server", "package": seedPkg.PURL, "version": "v1.0.0", "advisory_id": "GO-2026-0001",
			"severity": "high", "confidence": "high", "exposed_now": true, "state": "open",
			"first_seen": "2026-03-01T13:00:00Z", "last_changed": "2026-03-01T13:00:00Z",
			"url": fmt.Sprintf("https://upw.example.invalid/f/%d", ids.advisory),
		},
		{
			"id": float64(ids.jobFailure), "kind": "job_failure", "score": 5.0, "rank_reason": "job failed",
			"repo": "", "package": "", "version": "", "advisory_id": "",
			"severity": "", "confidence": "", "exposed_now": false, "state": "open",
			"first_seen": "2026-03-01T14:00:00Z", "last_changed": "2026-03-01T14:00:00Z",
			"url": fmt.Sprintf("https://upw.example.invalid/f/%d", ids.jobFailure),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings -json =\n%v\nwant\n%v", got, want)
	}
	// Keys come in the documented order.
	order := []string{`"id"`, `"kind"`, `"score"`, `"rank_reason"`, `"repo"`, `"package"`, `"version"`, `"advisory_id"`,
		`"severity"`, `"confidence"`, `"exposed_now"`, `"state"`, `"first_seen"`, `"last_changed"`, `"url"`}
	first := stdout[:strings.Index(stdout, "},")]
	last := -1
	for _, k := range order {
		i := strings.Index(first, k)
		if i <= last {
			t.Errorf("key %s out of order in %s", k, first)
		}
		last = i
	}
}

func TestFindings_JSONWithoutBaseURL(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	seedFindings(t, cfg)
	code, stdout, stderr := runFindings(t, "-json", "-config", cfg)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr %q", code, stderr)
	}
	if strings.Contains(stdout, `"url"`) {
		t.Errorf("stdout has a url key with no base URL configured:\n%s", stdout)
	}
}

func TestFindings_RepoFilter(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	seedFindings(t, cfg)
	code, stdout, stderr := runFindings(t, "-repo", "example-server", "-config", cfg)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "GO-2026-0001") || strings.Contains(stdout, "job_failure") {
		t.Errorf("stdout =\n%s\nwant the advisory finding only", stdout)
	}
}

func TestFindings_UnknownRepo(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	seedFindings(t, cfg)
	code, stdout, stderr := runFindings(t, "-repo", "nope", "-config", cfg)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "upw: unknown repo \"nope\"\n"; !strings.HasSuffix(stderr, want) {
		t.Errorf("stderr = %q, want it to end with %q", stderr, want)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestFindings_Suppressed(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	ids := seedFindings(t, cfg)
	a, err := openApp(t.Context(), cfg, slog.New(slog.DiscardHandler), noEnv)
	if err != nil {
		t.Fatalf("openApp() error = %v", err)
	}
	if err := a.db.SaveFindingState(t.Context(), ids.jobFailure, model.FindingState{
		State: model.StateDismissed, ChangedAt: seedDay, ChangedBy: "test"}); err != nil {
		t.Fatalf("SaveFindingState() error = %v", err)
	}
	if err := a.close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	_, stdout, _ := runFindings(t, "-config", cfg)
	if strings.Contains(stdout, "job_failure") {
		t.Errorf("default output shows the dismissed finding:\n%s", stdout)
	}
	_, stdout, _ = runFindings(t, "-suppressed", "-config", cfg)
	if !strings.Contains(stdout, "job_failure") || !strings.Contains(stdout, "dismissed") {
		t.Errorf("-suppressed output does not show the dismissed finding:\n%s", stdout)
	}
}

func TestFindings_Empty(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	if _, stdout, _ := runFindings(t, "-config", cfg); stdout != "no findings\n" {
		t.Errorf("table stdout = %q, want %q", stdout, "no findings\n")
	}
	if _, stdout, _ := runFindings(t, "-json", "-config", cfg); stdout != "[]\n" {
		t.Errorf("json stdout = %q, want %q", stdout, "[]\n")
	}
}

func TestFindings_TableIsTerminalSafe(t *testing.T) {
	t.Parallel()
	const raw = "\x1b]0;owned\x07"
	details := []model.FindingDetail{{
		Finding:  model.Finding{ID: 1, Kind: model.KindJobFailure, Fingerprint: "job_failure|" + raw, Score: 1},
		RepoSlug: "repo\x1b[2J",
	}}
	var out bytes.Buffer
	if err := writeFindingsText(&out, details, seedDay); err != nil {
		t.Fatalf("writeFindingsText() error = %v", err)
	}
	if strings.ContainsAny(out.String(), "\x1b\x07") {
		t.Errorf("output holds a raw control character: %q", out.String())
	}
}

func TestFindings_UsageErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{name: "help", args: []string{"-h"}, wantCode: 0},
		{name: "extra argument", args: []string{"extra"}, wantCode: 2},
		{name: "unknown flag", args: []string{"-bogus"}, wantCode: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if code, _, _ := runFindings(t, tt.args...); code != tt.wantCode {
				t.Errorf("findings(%q) exit code = %d, want %d", tt.args, code, tt.wantCode)
			}
		})
	}
}
