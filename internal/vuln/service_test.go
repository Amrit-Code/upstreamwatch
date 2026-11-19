package vuln_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
)

const (
	libPURL   = "pkg:golang/github.com/example-org/lib"
	otherPURL = "pkg:golang/github.com/example-org/other"
	toolPURL  = "pkg:golang/toolchain"
	imagePURL = "pkg:docker/library/golang"
	pypiPURL  = "pkg:pypi/example-lib"

	serverSlug = "example-server"
	adminSlug  = "example-admin"
	serverID   = int64(1)
	adminID    = int64(2)

	advLib   = "GO-2026-0001"
	advTool  = "GO-2026-0002"
	advLibB  = "GO-2026-0003"
	toolCVE  = "CVE-2026-0002"
	cursorKV = "osv.modified.Go"
)

// day returns a fixed time on day n of March 2026 (day 0 is 28 February).
func day(n int) time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, n-1) }

// at returns a fixed time on 1 April 2026 plus h hours.
func at(h int) time.Time {
	return time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(h) * time.Hour)
}

// runAt is the clock of a first run.
var runAt = time.Date(2026, 4, 2, 0, 10, 0, 0, time.UTC)

// interval builds a stored interval. A zero to leaves it open.
func interval(repoID int64, slug string, id, pkgID int64, path, purl, version string, direct model.Directness, from, to time.Time) model.RepoInterval {
	iv := model.DepInterval{
		ID:           id,
		ManifestPath: path,
		Package:      model.Package{PURL: purl},
		Version:      version,
		Direct:       direct,
		FromSHA:      "f" + version,
		From:         from,
	}
	if !to.IsZero() {
		iv.ToSHA, iv.To = "t"+version, to
	}
	return model.RepoInterval{RepoID: repoID, RepoSlug: slug, PackageID: pkgID, Interval: iv}
}

// fixtureIntervals are two repos: the server pins lib v1.2.0, the toolchain
// at 1.26 and a golang image; the admin pinned lib v1.2.0, then v1.3.0, and
// declares another package without a version.
func fixtureIntervals() []model.RepoInterval {
	return []model.RepoInterval{
		interval(serverID, serverSlug, 11, 101, "go.mod", libPURL, "v1.2.0", model.DirectYes, day(2), time.Time{}),
		interval(serverID, serverSlug, 12, 102, "go.mod", toolPURL, "1.26", model.DirectUnknown, day(1), time.Time{}),
		interval(serverID, serverSlug, 13, 103, "Dockerfile", imagePURL, "1.26-alpine", model.DirectYes, day(1), time.Time{}),
		interval(adminID, adminSlug, 21, 101, "go.mod", libPURL, "v1.2.0", model.DirectYes, day(1), day(5)),
		interval(adminID, adminSlug, 22, 101, "go.mod", libPURL, "v1.3.0", model.DirectYes, day(5), time.Time{}),
		interval(adminID, adminSlug, 23, 104, "go.mod", otherPURL, "", model.DirectYes, day(1), time.Time{}),
	}
}

// goRecord builds a GO advisory for purl, affected from 0 up to fixed.
func goRecord(id, purl, fixed string, sev model.SeverityLabel, published, modified time.Time) model.Advisory {
	return model.Advisory{
		ID:        id,
		Summary:   "example summary",
		Severity:  sev,
		Published: published,
		Modified:  modified,
		Affected: []model.AffectedPackage{{
			Package: model.Package{PURL: purl},
			Ranges: []model.AffectedRange{{
				Type:   model.RangeSemver,
				Events: []model.RangeEvent{{Introduced: "0"}, {Fixed: fixed}},
			}},
			SourceDB: "GO",
		}},
	}
}

// fixtureSource serves three GO advisories through the query batch and a Go
// modified list whose newest line is at(0).
func fixtureSource() *fakeSource {
	src := newFakeSource()
	lib := goRecord(advLib, libPURL, "1.2.5", model.SeverityHigh, day(3), day(9))
	tool := goRecord(advTool, toolPURL, "1.26.4", model.SeverityCritical, day(0), day(9))
	tool.Aliases = []string{toolCVE}
	libB := goRecord(advLibB, libPURL, "1.4.0", model.SeverityMedium, day(0), at(6))
	for _, a := range []model.Advisory{lib, tool, libB} {
		src.records[a.ID] = a
	}
	ref := func(a model.Advisory) model.AdvisoryRef { return model.AdvisoryRef{ID: a.ID, Modified: a.Modified} }
	src.queryRefs[model.PackageVersion{PURL: libPURL, Version: "v1.2.0"}] = []model.AdvisoryRef{ref(lib), ref(libB)}
	src.queryRefs[model.PackageVersion{PURL: libPURL, Version: "v1.3.0"}] = []model.AdvisoryRef{ref(libB)}
	src.queryRefs[model.PackageVersion{PURL: toolPURL, Version: "1.26"}] = []model.AdvisoryRef{ref(tool)}
	src.modified["Go"] = []model.AdvisoryRef{
		{ID: "GO-2026-0100", Modified: at(0)},
		{ID: "GO-2026-0099", Modified: at(-24)},
	}
	return src
}

// prepend puts newer lines at the head of an ecosystem's modified list.
func (f *fakeSource) prepend(eco string, refs ...model.AdvisoryRef) {
	f.modified[eco] = append(slices.Clone(refs), f.modified[eco]...)
}

// harness is one service under test with its fakes and clock.
type harness struct {
	src   *fakeSource
	store *fakeStore
	kev   *fakeFeed
	epss  *fakeFeed
	now   time.Time
	svc   *vuln.Service
}

// newHarness builds a service over the fixture; kev and epss may be nil.
func newHarness(t *testing.T, kev, epss *fakeFeed, opts vuln.Options) *harness {
	t.Helper()
	h := &harness{src: fixtureSource(), store: newFakeStore(fixtureIntervals()...), kev: kev, epss: epss, now: runAt}
	opts.Now = func() time.Time { return h.now }
	h.svc = vuln.NewService(h.src, feed(kev), feed(epss), h.store, h.store, h.store, opts, slog.New(slog.DiscardHandler))
	return h
}

// feed returns f as an ExploitSignals, a nil interface when f is nil.
func feed(f *fakeFeed) vuln.ExploitSignals {
	if f == nil {
		return nil
	}
	return f
}

// run runs the service over scope and fails the test on an error.
func (h *harness) run(t *testing.T, scope model.Scope) model.Stats {
	t.Helper()
	stats, err := h.svc.Run(t.Context(), scope)
	if err != nil {
		t.Fatalf("Run(%+v) error = %v", scope, err)
	}
	return stats
}

// checkStats compares the named counters of got with want.
func checkStats(t *testing.T, got model.Stats, want map[string]int64) {
	t.Helper()
	for k, w := range want {
		if got[k] != w {
			t.Errorf("stats[%q] = %d, want %d", k, got[k], w)
		}
	}
}

func fingerprint(slug, purl, id string) string {
	return "advisory|" + slug + "|" + purl + "|" + id
}

func TestService_Name(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil, nil, vuln.Options{})
	if got := h.svc.Name(); got != "vulns" {
		t.Errorf("Name() = %q, want %q", got, "vulns")
	}
}

func TestRun_FirstRun(t *testing.T) {
	t.Parallel()
	kev := &fakeFeed{signals: []model.Signal{{Kind: model.SignalKEV, CVE: toolCVE, KEVAddedAt: day(4)}}}
	h := newHarness(t, kev, nil, vuln.Options{})
	stats := h.run(t, model.Scope{})

	checkStats(t, stats, map[string]int64{
		"intervals": 6, "unversioned": 1, "pairs_new": 3, "query_refs": 3, "delta_refs": 0,
		"fetched": 3, "fetch_not_found": 0, "delta_irrelevant": 0, "advisories_saved": 3,
		"kev_updated": 1, "epss_updated": 0, "kev_errors": 0, "epss_errors": 0,
		"matches": 6, "matches_open": 4, "findings_upserted": 5, "findings_resolved": 0,
	})
	if len(stats) != 17 {
		t.Errorf("len(stats) = %d, want 17 counters: %v", len(stats), stats)
	}

	wantPairs := [][]model.PackageVersion{{
		{PURL: libPURL, Version: "v1.2.0"},
		{PURL: libPURL, Version: "v1.3.0"},
		{PURL: toolPURL, Version: "1.26"},
	}}
	if !reflect.DeepEqual(h.src.queryCalls, wantPairs) {
		t.Errorf("QueryBatch calls = %v, want %v (Go pairs only)", h.src.queryCalls, wantPairs)
	}
	if len(h.store.queried) != 3 {
		t.Errorf("queried pairs = %v, want the 3 sent", h.store.queried)
	}
	for pv, ts := range h.store.queried {
		if !ts.Equal(runAt) {
			t.Errorf("queried %v at %v, want %v", pv, ts, runAt)
		}
	}

	wantMod := []modCall{{ecosystem: "Go"}}
	if !reflect.DeepEqual(h.src.modCalls, wantMod) {
		t.Errorf("ModifiedSince calls = %v, want %v", h.src.modCalls, wantMod)
	}
	if got, want := h.store.kv[cursorKV], "2026-04-01T00:00:00Z"; got != want {
		t.Errorf("kv[%s] = %q, want %q (newest delta line)", cursorKV, got, want)
	}
	wantFetch := []string{advLib, advTool, advLibB}
	if !reflect.DeepEqual(h.src.fetchCalls, wantFetch) {
		t.Errorf("Fetch calls = %v, want %v (no delta record fetched)", h.src.fetchCalls, wantFetch)
	}

	var repos []int64
	for _, c := range h.store.replaceCalls {
		repos = append(repos, c.repoID)
	}
	if want := []int64{adminID, serverID}; !reflect.DeepEqual(repos, want) {
		t.Errorf("ReplaceMatches repos = %v, want %v", repos, want)
	}
	type mk struct {
		dep int64
		adv string
	}
	gotMatches := map[mk]model.AdvisoryMatch{}
	for _, ms := range h.store.matches {
		for _, m := range ms {
			gotMatches[mk{m.DepVersionID, m.AdvisoryID}] = m
		}
	}
	wantKeys := []mk{{11, advLib}, {11, advLibB}, {12, advTool}, {21, advLib}, {21, advLibB}, {22, advLibB}}
	if len(gotMatches) != len(wantKeys) {
		t.Errorf("matches = %v, want keys %v", gotMatches, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := gotMatches[k]; !ok {
			t.Errorf("no match for interval %d, advisory %s", k.dep, k.adv)
		}
	}
	// The toolchain interval pins the 1.26 line: the stdlib advisory applies
	// with medium confidence (checklist item 6).
	tm := gotMatches[mk{12, advTool}]
	if tm.Confidence != model.ConfidenceMedium || tm.FixedVersion != "1.26.4" || tm.RepoID != serverID {
		t.Errorf("toolchain match = %+v, want medium confidence, fixed 1.26.4, repo %d", tm, serverID)
	}
	if lm := gotMatches[mk{11, advLib}]; !lm.ExposedFrom.Equal(day(3)) || !lm.IsOpen() {
		t.Errorf("lib match exposure = %v..%v, want from the publication %v, open", lm.ExposedFrom, lm.ExposedTo, day(3))
	}
	if cm := gotMatches[mk{21, advLib}]; !cm.ExposedFrom.Equal(day(3)) || !cm.ExposedTo.Equal(day(5)) {
		t.Errorf("closed match exposure = %v..%v, want %v..%v", cm.ExposedFrom, cm.ExposedTo, day(3), day(5))
	}

	want := []model.Finding{
		{Fingerprint: fingerprint(adminSlug, libPURL, advLib), RepoID: adminID, PackageID: 101, DepVersionID: 21,
			Score: 80, RankReason: "high 70; direct +10; fixed in 1.2.5"},
		{Fingerprint: fingerprint(adminSlug, libPURL, advLibB), RepoID: adminID, PackageID: 101, DepVersionID: 22,
			Score: 70, RankReason: "medium 40; direct +10; exposed now +20; fixed in 1.4.0"},
		{Fingerprint: fingerprint(serverSlug, libPURL, advLib), RepoID: serverID, PackageID: 101, DepVersionID: 11,
			Score: 100, RankReason: "high 70; direct +10; exposed now +20; fixed in 1.2.5"},
		{Fingerprint: fingerprint(serverSlug, libPURL, advLibB), RepoID: serverID, PackageID: 101, DepVersionID: 11,
			Score: 70, RankReason: "medium 40; direct +10; exposed now +20; fixed in 1.4.0"},
		{Fingerprint: fingerprint(serverSlug, toolPURL, advTool), RepoID: serverID, PackageID: 102, DepVersionID: 12,
			Score: 130, RankReason: "critical 90; KEV +30; exposed now +20; confidence medium -10; fixed in 1.26.4"},
	}
	if len(h.store.upserts) != len(want) {
		t.Fatalf("UpsertFinding calls = %d, want %d: %+v", len(h.store.upserts), len(want), h.store.upserts)
	}
	for i, w := range want {
		got := h.store.upserts[i]
		w.Kind = model.KindAdvisory
		w.AdvisoryID = w.Fingerprint[strings.LastIndexByte(w.Fingerprint, '|')+1:]
		w.LastChangedAt = runAt
		if !reflect.DeepEqual(got, w) {
			t.Errorf("UpsertFinding #%d =\n %+v\nwant\n %+v", i, got, w)
		}
	}
}

func TestRun_SecondRunDelta(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil, nil, vuln.Options{})
	h.run(t, model.Scope{})
	h.src.resetCalls()

	changed := h.src.records[advLib]
	changed.Modified = at(48)
	h.src.records[advLib] = changed
	// advLibB's line repeats the time already stored for it.
	h.src.prepend("Go", model.AdvisoryRef{ID: advLib, Modified: at(48)}, model.AdvisoryRef{ID: advLibB, Modified: at(6)})
	h.now = runAt.Add(6 * time.Hour)
	stats := h.run(t, model.Scope{})

	if len(h.src.queryCalls) != 0 {
		t.Errorf("QueryBatch calls = %v, want none", h.src.queryCalls)
	}
	if want := []modCall{{ecosystem: "Go", since: at(0)}}; !reflect.DeepEqual(h.src.modCalls, want) {
		t.Errorf("ModifiedSince calls = %v, want %v", h.src.modCalls, want)
	}
	if want := []string{advLib}; !reflect.DeepEqual(h.src.fetchCalls, want) {
		t.Errorf("Fetch calls = %v, want %v", h.src.fetchCalls, want)
	}
	if got, want := h.store.kv[cursorKV], "2026-04-03T00:00:00Z"; got != want {
		t.Errorf("kv[%s] = %q, want %q", cursorKV, got, want)
	}
	checkStats(t, stats, map[string]int64{
		"pairs_new": 0, "query_refs": 0, "delta_refs": 2, "fetched": 1, "advisories_saved": 1,
		"matches": 6, "findings_upserted": 5, "findings_resolved": 0,
	})
	if got := h.store.advisories[advLib].Modified; !got.Equal(at(48)) {
		t.Errorf("stored %s modified = %v, want %v", advLib, got, at(48))
	}
}

func TestRun_NewPairOnly(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil, nil, vuln.Options{})
	h.run(t, model.Scope{})
	h.src.resetCalls()

	h.store.intervals = append(h.store.intervals,
		interval(serverID, serverSlug, 14, 101, "tools/go.mod", libPURL, "v1.2.1", model.DirectYes, day(8), time.Time{}))
	h.now = runAt.Add(6 * time.Hour)
	stats := h.run(t, model.Scope{})

	want := [][]model.PackageVersion{{{PURL: libPURL, Version: "v1.2.1"}}}
	if !reflect.DeepEqual(h.src.queryCalls, want) {
		t.Errorf("QueryBatch calls = %v, want %v", h.src.queryCalls, want)
	}
	checkStats(t, stats, map[string]int64{"pairs_new": 1, "intervals": 7})
}

func TestRun_ResolveVanished(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil, nil, vuln.Options{})
	h.run(t, model.Scope{})
	h.src.resetCalls()
	h.store.resetCalls()

	withdrawn := h.src.records[advLibB]
	withdrawn.Modified, withdrawn.Withdrawn = at(48), at(47)
	h.src.records[advLibB] = withdrawn
	h.src.prepend("Go", model.AdvisoryRef{ID: advLibB, Modified: at(48)})
	h.now = runAt.Add(6 * time.Hour)
	stats := h.run(t, model.Scope{})

	for repo, ms := range h.store.matches {
		for _, m := range ms {
			if m.AdvisoryID == advLibB {
				t.Errorf("repo %d still has a match of withdrawn %s: %+v", repo, advLibB, m)
			}
		}
	}
	wantResolved := []string{fingerprint(adminSlug, libPURL, advLibB), fingerprint(serverSlug, libPURL, advLibB)}
	slices.Sort(h.store.resolved)
	if !reflect.DeepEqual(h.store.resolved, wantResolved) {
		t.Errorf("resolved = %v, want %v", h.store.resolved, wantResolved)
	}
	for _, fp := range wantResolved {
		if f, _ := h.store.finding(fp); !f.ResolvedAt.Equal(h.now) {
			t.Errorf("finding %s resolved at %v, want %v", fp, f.ResolvedAt, h.now)
		}
	}
	checkStats(t, stats, map[string]int64{"matches": 3, "findings_upserted": 3, "findings_resolved": 2})
}

func TestRun_SignalsDaily(t *testing.T) {
	t.Parallel()

	t.Run("refreshed once a day", func(t *testing.T) {
		t.Parallel()
		kev := &fakeFeed{signals: []model.Signal{{Kind: model.SignalKEV, CVE: toolCVE, KEVAddedAt: day(4)}}}
		epss := &fakeFeed{signals: []model.Signal{{Kind: model.SignalEPSS, CVE: toolCVE, EPSS: 0.12, EPSSPercentile: 0.71}}}
		h := newHarness(t, kev, epss, vuln.Options{})

		steps := []struct {
			after     time.Duration
			wantCalls int
			wantKEV   int64
		}{
			{0, 1, 1},
			{time.Hour, 1, 0},
			{21 * time.Hour, 2, 1},
		}
		for i, st := range steps {
			h.now = h.now.Add(st.after)
			stats := h.run(t, model.Scope{})
			if kev.calls != st.wantCalls || epss.calls != st.wantCalls {
				t.Errorf("run %d: Refresh calls kev %d epss %d, want %d each", i+1, kev.calls, epss.calls, st.wantCalls)
			}
			checkStats(t, stats, map[string]int64{"kev_updated": st.wantKEV, "epss_updated": st.wantKEV})
		}
		if got, want := h.store.kv["signals.epss.refreshed_at"], "2026-04-02T22:10:00Z"; got != want {
			t.Errorf("epss refreshed_at = %q, want %q", got, want)
		}
		f, _ := h.store.finding(fingerprint(serverSlug, toolPURL, advTool))
		if want := "critical 90; KEV +30; EPSS p71 +14; exposed now +20; confidence medium -10; fixed in 1.26.4"; f.RankReason != want {
			t.Errorf("toolchain rank reason = %q, want %q", f.RankReason, want)
		}
	})

	t.Run("failing EPSS feed", func(t *testing.T) {
		t.Parallel()
		kev := &fakeFeed{}
		feedErr := errors.New("epss feed: status 503")
		epss := &fakeFeed{err: feedErr}
		h := newHarness(t, kev, epss, vuln.Options{})
		stats, err := h.svc.Run(t.Context(), model.Scope{})
		if !errors.Is(err, feedErr) {
			t.Fatalf("Run() error = %v, want it to wrap %v", err, feedErr)
		}
		checkStats(t, stats, map[string]int64{"epss_errors": 1, "kev_errors": 0, "matches": 6, "findings_upserted": 5})
		if len(h.store.replaceCalls) != 2 || len(h.store.upserts) != 5 {
			t.Errorf("ReplaceMatches calls %d, upserts %d, want 2 and 5", len(h.store.replaceCalls), len(h.store.upserts))
		}
		if _, ok := h.store.kv["signals.epss.refreshed_at"]; ok {
			t.Errorf("epss refreshed_at stored after a failed refresh")
		}
		if _, ok := h.store.kv["signals.kev.refreshed_at"]; !ok {
			t.Errorf("kev refreshed_at not stored")
		}
	})
}

func TestRun_ExposureBasisPin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		basis string
		want  time.Time
	}{
		{"pin", day(2)},
		{"published", day(3)},
		{"", day(3)},
	}
	for _, tt := range tests {
		t.Run("basis "+tt.basis, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil, nil, vuln.Options{ExposureBasis: tt.basis})
			h.run(t, model.Scope{})
			var got model.AdvisoryMatch
			for _, m := range h.store.matches[serverID] {
				if m.DepVersionID == 11 && m.AdvisoryID == advLib {
					got = m
				}
			}
			if !got.ExposedFrom.Equal(tt.want) {
				t.Errorf("ExposedFrom = %v, want %v", got.ExposedFrom, tt.want)
			}
		})
	}
}

func TestRun_ScopeOneRepo(t *testing.T) {
	t.Parallel()

	t.Run("one repo", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil, nil, vuln.Options{})
		h.run(t, model.Scope{})
		h.src.resetCalls()
		h.store.resetCalls()
		h.store.intervals = append(h.store.intervals,
			interval(adminID, adminSlug, 24, 102, "go.mod", toolPURL, "1.25", model.DirectUnknown, day(6), time.Time{}),
			interval(serverID, serverSlug, 14, 102, "tools/go.mod", toolPURL, "1.27", model.DirectUnknown, day(6), time.Time{}))
		withdrawn := h.src.records[advLibB]
		withdrawn.Modified, withdrawn.Withdrawn = at(48), at(47)
		h.src.records[advLibB] = withdrawn
		h.src.prepend("Go", model.AdvisoryRef{ID: advLibB, Modified: at(48)})
		h.now = runAt.Add(6 * time.Hour)

		stats := h.run(t, model.Scope{RepoSlug: serverSlug})
		want := [][]model.PackageVersion{{{PURL: toolPURL, Version: "1.27"}}}
		if !reflect.DeepEqual(h.src.queryCalls, want) {
			t.Errorf("QueryBatch calls = %v, want %v (the repo's new pair only)", h.src.queryCalls, want)
		}
		if len(h.src.modCalls) != 1 {
			t.Errorf("ModifiedSince calls = %v, want the global delta read", h.src.modCalls)
		}
		if len(h.store.replaceCalls) != 1 || h.store.replaceCalls[0].repoID != serverID {
			t.Errorf("ReplaceMatches calls = %+v, want one for repo %d", h.store.replaceCalls, serverID)
		}
		for _, f := range h.store.upserts {
			if f.RepoID != serverID {
				t.Errorf("finding %s upserted for repo %d, want only repo %d", f.Fingerprint, f.RepoID, serverID)
			}
		}
		if want := []string{fingerprint(serverSlug, libPURL, advLibB)}; !reflect.DeepEqual(h.store.resolved, want) {
			t.Errorf("resolved = %v, want %v (the admin finding is out of scope)", h.store.resolved, want)
		}
		checkStats(t, stats, map[string]int64{"intervals": 4, "unversioned": 0, "pairs_new": 1})
	})

	t.Run("unknown slug", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil, nil, vuln.Options{})
		_, err := h.svc.Run(t.Context(), model.Scope{RepoSlug: "example-missing"})
		if !errors.Is(err, model.ErrNotFound) {
			t.Errorf("Run() error = %v, want it to wrap model.ErrNotFound", err)
		}
		if len(h.src.queryCalls)+len(h.src.modCalls)+len(h.store.replaceCalls) != 0 {
			t.Errorf("calls after an unknown slug: query %v, delta %v, replace %v",
				h.src.queryCalls, h.src.modCalls, h.store.replaceCalls)
		}
	})
}

func TestRun_FetchErrorsCollected(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil, nil, vuln.Options{EcosystemsExtra: []string{"PyPI"}})
	h.run(t, model.Scope{})
	h.src.resetCalls()
	h.store.resetCalls()

	fetchErr := errors.New("GET https://user:ghp_abcdefghijklmnopqrstuvwxyz0123@example.com/v1/vulns: status 500")
	h.src.fetchErr[advLib] = fetchErr
	h.src.records["GO-2026-0200"] = goRecord("GO-2026-0200", "pkg:golang/github.com/example-org/unused", "1.0.0",
		model.SeverityLow, day(0), at(30))
	h.src.prepend("Go",
		model.AdvisoryRef{ID: "GO-2026-0300", Modified: at(40)}, // not found
		model.AdvisoryRef{ID: advLib, Modified: at(36)},
		model.AdvisoryRef{ID: "GO-2026-0200", Modified: at(30)},
	)
	h.src.modErr["PyPI"] = errors.New("modified_id.csv: status 503")
	h.now = runAt.Add(6 * time.Hour)

	stats, err := h.svc.Run(t.Context(), model.Scope{})
	if !errors.Is(err, fetchErr) {
		t.Fatalf("Run() error = %v, want it to wrap the fetch error", err)
	}
	if strings.Contains(err.Error(), "ghp_") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("Run() error text %q is not redacted", err)
	}
	if !strings.Contains(err.Error(), "PyPI") {
		t.Errorf("Run() error %q does not name the failed PyPI delta", err)
	}
	if got := h.store.kv[cursorKV]; got != "2026-04-01T00:00:00Z" {
		t.Errorf("kv[%s] = %q, want it unchanged after a failed fetch", cursorKV, got)
	}
	if _, ok := h.store.kv["osv.modified.PyPI"]; ok {
		t.Errorf("PyPI cursor stored after a failed delta download")
	}
	if _, ok := h.store.advisories["GO-2026-0200"]; ok {
		t.Errorf("irrelevant delta record saved")
	}
	if want := []modCall{{ecosystem: "Go", since: at(0)}, {ecosystem: "PyPI"}}; !reflect.DeepEqual(h.src.modCalls, want) {
		t.Errorf("ModifiedSince calls = %v, want %v", h.src.modCalls, want)
	}
	checkStats(t, stats, map[string]int64{
		"delta_refs": 3, "fetched": 1, "fetch_not_found": 1, "delta_irrelevant": 1, "advisories_saved": 0,
		"matches": 6, "findings_upserted": 5,
	})
}

func TestRun_QueryRefFetchRetried(t *testing.T) {
	t.Parallel()

	t.Run("transient fetch error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil, nil, vuln.Options{})
		fetchErr := errors.New("GET /v1/vulns: status 503")
		h.src.fetchErr[advLib] = fetchErr
		if _, err := h.svc.Run(t.Context(), model.Scope{}); !errors.Is(err, fetchErr) {
			t.Fatalf("first Run() error = %v, want it to wrap %v", err, fetchErr)
		}
		if len(h.store.queried) != 0 {
			t.Errorf("queried pairs after a failed query-ref fetch = %v, want none", h.store.queried)
		}

		h.src.resetCalls()
		delete(h.src.fetchErr, advLib)
		h.now = runAt.Add(6 * time.Hour)
		stats := h.run(t, model.Scope{})
		want := [][]model.PackageVersion{{
			{PURL: libPURL, Version: "v1.2.0"},
			{PURL: libPURL, Version: "v1.3.0"},
			{PURL: toolPURL, Version: "1.26"},
		}}
		if !reflect.DeepEqual(h.src.queryCalls, want) {
			t.Errorf("second run QueryBatch calls = %v, want %v", h.src.queryCalls, want)
		}
		if want := []string{advLib}; !reflect.DeepEqual(h.src.fetchCalls, want) {
			t.Errorf("second run Fetch calls = %v, want %v (the others are stored)", h.src.fetchCalls, want)
		}
		if _, ok := h.store.advisories[advLib]; !ok {
			t.Errorf("%s not saved on the retry", advLib)
		}
		if len(h.store.queried) != 3 {
			t.Errorf("queried pairs after the retry = %v, want the 3 sent", h.store.queried)
		}
		for pv, ts := range h.store.queried {
			if !ts.Equal(h.now) {
				t.Errorf("queried %v at %v, want %v", pv, ts, h.now)
			}
		}
		checkStats(t, stats, map[string]int64{"pairs_new": 3, "fetched": 1, "findings_upserted": 5})
	})

	t.Run("not found counts as fetched", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil, nil, vuln.Options{})
		delete(h.src.records, advLib)
		stats := h.run(t, model.Scope{})
		if len(h.store.queried) != 3 {
			t.Errorf("queried pairs = %v, want the 3 sent", h.store.queried)
		}
		checkStats(t, stats, map[string]int64{"fetch_not_found": 1, "fetched": 2})
	})
}

func TestRun_StopsOnStoreAndQueryFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name  string
		setup func(h *harness)
	}{
		{"list intervals", func(h *harness) { h.store.listErr = boom }},
		{"query batch", func(h *harness) { h.src.queryErr = boom }},
		{"replace matches", func(h *harness) { h.store.replaceErr = boom }},
		{"upsert finding", func(h *harness) { h.store.upsertErr = boom }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil, nil, vuln.Options{})
			tt.setup(h)
			_, err := h.svc.Run(t.Context(), model.Scope{})
			if !errors.Is(err, boom) {
				t.Fatalf("Run() error = %v, want it to wrap %v", err, boom)
			}
			if len(h.store.upserts) > 1 {
				t.Errorf("UpsertFinding calls = %d after a stopping failure, want at most the failing one", len(h.store.upserts))
			}
			if tt.name == "query batch" && len(h.store.queried) != 0 {
				t.Errorf("pairs marked queried after a failed query batch: %v", h.store.queried)
			}
		})
	}
}

func TestRun_Cancelled(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil, nil, vuln.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := h.svc.Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(h.src.queryCalls)+len(h.store.replaceCalls)+len(h.store.upserts) != 0 {
		t.Errorf("work done after cancellation: query %d, replace %d, upserts %d",
			len(h.src.queryCalls), len(h.store.replaceCalls), len(h.store.upserts))
	}
}
