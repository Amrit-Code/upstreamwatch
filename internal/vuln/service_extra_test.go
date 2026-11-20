package vuln_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
)

// failingStore wraps fakeStore and fails the calls whose error is set.
type failingStore struct {
	*fakeStore
	listAdvErr error
	markErr    error
	resolveErr error
}

func (s *failingStore) ListAdvisories(ctx context.Context) ([]model.Advisory, error) {
	if s.listAdvErr != nil {
		return nil, s.listAdvErr
	}
	return s.fakeStore.ListAdvisories(ctx)
}

func (s *failingStore) MarkQueried(ctx context.Context, pvs []model.PackageVersion, at time.Time) error {
	if s.markErr != nil {
		return s.markErr
	}
	return s.fakeStore.MarkQueried(ctx, pvs, at)
}

func (s *failingStore) ResolveFinding(ctx context.Context, fp string, at time.Time) error {
	if s.resolveErr != nil {
		return s.resolveErr
	}
	return s.fakeStore.ResolveFinding(ctx, fp, at)
}

// lockedBuffer is a bytes.Buffer the logger may write from any goroutine.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestRun_StopsOnOtherStoreFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")

	t.Run("list advisories", func(t *testing.T) {
		t.Parallel()
		fs := newFakeStore(fixtureIntervals()...)
		w := &failingStore{fakeStore: fs, listAdvErr: boom}
		svc := vuln.NewService(fixtureSource(), nil, nil, w, w, w, vuln.Options{Now: func() time.Time { return runAt }}, discardLogger())
		if _, err := svc.Run(t.Context(), model.Scope{}); !errors.Is(err, boom) {
			t.Fatalf("Run() error = %v, want it to wrap %v", err, boom)
		}
		if len(fs.replaceCalls) != 0 || len(fs.upserts) != 0 {
			t.Errorf("ReplaceMatches calls %d, upserts %d after a failed ListAdvisories, want none", len(fs.replaceCalls), len(fs.upserts))
		}
	})

	t.Run("mark queried", func(t *testing.T) {
		t.Parallel()
		fs := newFakeStore(fixtureIntervals()...)
		w := &failingStore{fakeStore: fs, markErr: boom}
		svc := vuln.NewService(fixtureSource(), nil, nil, w, w, w, vuln.Options{Now: func() time.Time { return runAt }}, discardLogger())
		if _, err := svc.Run(t.Context(), model.Scope{}); !errors.Is(err, boom) {
			t.Fatalf("Run() error = %v, want it to wrap %v", err, boom)
		}
		if len(fs.replaceCalls) != 0 || len(fs.upserts) != 0 {
			t.Errorf("ReplaceMatches calls %d, upserts %d after a failed MarkQueried, want none", len(fs.replaceCalls), len(fs.upserts))
		}
	})

	t.Run("resolve finding", func(t *testing.T) {
		t.Parallel()
		src := fixtureSource()
		fs := newFakeStore(fixtureIntervals()...)
		w := &failingStore{fakeStore: fs}
		now := runAt
		svc := vuln.NewService(src, nil, nil, w, w, w, vuln.Options{Now: func() time.Time { return now }}, discardLogger())
		if _, err := svc.Run(t.Context(), model.Scope{}); err != nil {
			t.Fatalf("first Run() error = %v", err)
		}
		withdrawn := src.records[advLibB]
		withdrawn.Modified, withdrawn.Withdrawn = at(48), at(47)
		src.records[advLibB] = withdrawn
		src.prepend("Go", model.AdvisoryRef{ID: advLibB, Modified: at(48)})
		w.resolveErr = boom
		now = runAt.Add(6 * time.Hour)
		if _, err := svc.Run(t.Context(), model.Scope{}); !errors.Is(err, boom) {
			t.Fatalf("second Run() error = %v, want it to wrap %v", err, boom)
		}
	})
}

func TestRun_StatsPresentOnError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		scope model.Scope
		setup func(*fakeStore)
	}{
		{name: "unknown slug", scope: model.Scope{RepoSlug: "example-missing"}, setup: func(*fakeStore) {}},
		{name: "list intervals fails", setup: func(s *fakeStore) { s.listErr = errors.New("boom") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil, nil, vuln.Options{})
			tt.setup(h.store)
			stats, err := h.svc.Run(t.Context(), tt.scope)
			if err == nil {
				t.Fatal("Run() error = nil, want an error")
			}
			if len(stats) != 17 {
				t.Errorf("len(stats) = %d, want all 17 counters: %v", len(stats), stats)
			}
			for k, v := range stats {
				if v != 0 {
					t.Errorf("stats[%q] = %d, want 0", k, v)
				}
			}
		})
	}
}

// presetHarness is a service whose store already holds the advisories and
// has every pair queried, so a run only matches and writes findings.
func presetHarness(t *testing.T, intervals []model.RepoInterval, advisories ...model.Advisory) (*fakeStore, *vuln.Service) {
	t.Helper()
	fs := newFakeStore(intervals...)
	for _, a := range advisories {
		fs.advisories[a.ID] = a
	}
	for _, ri := range intervals {
		fs.queried[model.PackageVersion{PURL: ri.Interval.Package.PURL, Version: ri.Interval.Version}] = day(0)
	}
	svc := vuln.NewService(newFakeSource(), nil, nil, fs, fs, fs, vuln.Options{Now: func() time.Time { return runAt }}, discardLogger())
	return fs, svc
}

func TestRun_FindingLinkDirectAndConfidence(t *testing.T) {
	t.Parallel()
	libAdv := goRecord(advLib, libPURL, "1.2.0", model.SeverityHigh, time.Time{}, day(9))
	toolAdv := model.Advisory{
		ID: advTool, Severity: model.SeverityCritical, Modified: day(9),
		Affected: []model.AffectedPackage{{
			Package: model.Package{PURL: toolPURL},
			Ranges: []model.AffectedRange{{Type: model.RangeSemver,
				Events: []model.RangeEvent{{Introduced: "1.26.0-0"}, {Fixed: "1.26.4"}}}},
			SourceDB: "GO",
		}},
	}
	tests := []struct {
		name      string
		intervals []model.RepoInterval
		adv       model.Advisory
		wantDep   int64
		wantRank  model.AdvisoryRankInput
		wantFixed string
	}{
		{
			name: "two closed intervals link the one that closed last",
			intervals: []model.RepoInterval{
				interval(serverID, serverSlug, 31, 101, "go.mod", libPURL, "v1.0.0", model.DirectNo, day(1), day(3)),
				interval(serverID, serverSlug, 32, 101, "go.mod", libPURL, "v1.1.0", model.DirectNo, day(3), day(6)),
			},
			adv: libAdv, wantDep: 32, wantFixed: "1.2.0",
			wantRank: model.AdvisoryRankInput{Severity: model.SeverityHigh, Confidence: model.ConfidenceHigh},
		},
		{
			name: "two open intervals link the one that opened last",
			intervals: []model.RepoInterval{
				interval(serverID, serverSlug, 33, 101, "tools/go.mod", libPURL, "v1.1.0", model.DirectNo, day(4), time.Time{}),
				interval(serverID, serverSlug, 34, 101, "go.mod", libPURL, "v1.0.0", model.DirectNo, day(2), time.Time{}),
			},
			adv: libAdv, wantDep: 33, wantFixed: "1.2.0",
			wantRank: model.AdvisoryRankInput{Severity: model.SeverityHigh, CurrentlyExposed: true, Confidence: model.ConfidenceHigh},
		},
		{
			name: "a closed direct interval does not make an open indirect one direct",
			intervals: []model.RepoInterval{
				interval(serverID, serverSlug, 41, 101, "go.mod", libPURL, "v1.0.0", model.DirectYes, day(1), day(3)),
				interval(serverID, serverSlug, 42, 101, "go.mod", libPURL, "v1.1.0", model.DirectNo, day(3), time.Time{}),
			},
			adv: libAdv, wantDep: 42, wantFixed: "1.2.0",
			wantRank: model.AdvisoryRankInput{Severity: model.SeverityHigh, CurrentlyExposed: true, Confidence: model.ConfidenceHigh},
		},
		{
			name: "historical only: any direct interval counts",
			intervals: []model.RepoInterval{
				interval(serverID, serverSlug, 51, 101, "go.mod", libPURL, "v1.0.0", model.DirectYes, day(1), day(3)),
				interval(serverID, serverSlug, 52, 101, "go.mod", libPURL, "v1.1.0", model.DirectNo, day(3), day(5)),
			},
			adv: libAdv, wantDep: 52, wantFixed: "1.2.0",
			wantRank: model.AdvisoryRankInput{Severity: model.SeverityHigh, Direct: model.DirectYes, Confidence: model.ConfidenceHigh},
		},
		{
			name: "confidence comes from the open matches only",
			intervals: []model.RepoInterval{
				// go1.26.1 is an exact pin (high); the open 1.26 line floats (medium).
				interval(serverID, serverSlug, 61, 102, "Dockerfile", toolPURL, "go1.26.1", model.DirectUnknown, day(1), day(3)),
				interval(serverID, serverSlug, 62, 102, "go.mod", toolPURL, "1.26", model.DirectUnknown, day(3), time.Time{}),
			},
			adv: toolAdv, wantDep: 62, wantFixed: "1.26.4",
			wantRank: model.AdvisoryRankInput{Severity: model.SeverityCritical, CurrentlyExposed: true, Confidence: model.ConfidenceMedium},
		},
		{
			name: "historical only: the best confidence among all matches",
			intervals: []model.RepoInterval{
				interval(serverID, serverSlug, 71, 102, "Dockerfile", toolPURL, "go1.26.1", model.DirectUnknown, day(1), day(3)),
				interval(serverID, serverSlug, 72, 102, "go.mod", toolPURL, "1.26", model.DirectUnknown, day(1), day(2)),
			},
			adv: toolAdv, wantDep: 71, wantFixed: "1.26.4",
			wantRank: model.AdvisoryRankInput{Severity: model.SeverityCritical, Confidence: model.ConfidenceHigh},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs, svc := presetHarness(t, tt.intervals, tt.adv)
			if _, err := svc.Run(t.Context(), model.Scope{}); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if len(fs.upserts) != 1 {
				t.Fatalf("UpsertFinding calls = %d, want 1: %+v", len(fs.upserts), fs.upserts)
			}
			got := fs.upserts[0]
			score, reason := model.RankAdvisory(tt.wantRank)
			reason += "; fixed in " + tt.wantFixed
			purl := tt.intervals[0].Interval.Package.PURL
			want := model.Finding{
				Kind: model.KindAdvisory, RepoID: serverID, PackageID: tt.intervals[0].PackageID,
				DepVersionID: tt.wantDep, AdvisoryID: tt.adv.ID, Fingerprint: fingerprint(serverSlug, purl, tt.adv.ID),
				Score: score, RankReason: reason, LastChangedAt: runAt,
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("UpsertFinding =\n %+v\nwant\n %+v", got, want)
			}
		})
	}
}

func TestRun_FirstAffectedEntryInRecordOrder(t *testing.T) {
	t.Parallel()
	listEntry := func(versions ...string) model.AffectedPackage {
		return model.AffectedPackage{Package: model.Package{PURL: libPURL}, Versions: versions, SourceDB: "GO"}
	}
	rangeEntry := model.AffectedPackage{
		Package: model.Package{PURL: libPURL},
		Ranges: []model.AffectedRange{{Type: model.RangeSemver,
			Events: []model.RangeEvent{{Introduced: "0"}, {Fixed: "1.3.0"}}}},
		SourceDB: "GO",
	}
	otherEntry := model.AffectedPackage{
		Package: model.Package{PURL: otherPURL},
		Ranges: []model.AffectedRange{{Type: model.RangeSemver,
			Events: []model.RangeEvent{{Introduced: "0"}}}},
		SourceDB: "GO",
	}
	tests := []struct {
		name      string
		affected  []model.AffectedPackage
		wantConf  model.Confidence
		wantFixed string
	}{
		{"versions-list entry first wins", []model.AffectedPackage{listEntry("v1.2.0"), rangeEntry}, model.ConfidenceLow, ""},
		{"range entry first wins", []model.AffectedPackage{rangeEntry, listEntry("v1.2.0")}, model.ConfidenceHigh, "1.3.0"},
		{"unaffected first entry is passed over", []model.AffectedPackage{listEntry("v1.0.0"), rangeEntry}, model.ConfidenceHigh, "1.3.0"},
		{"another package's entry is passed over", []model.AffectedPackage{otherEntry, listEntry("v1.2.0")}, model.ConfidenceLow, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := model.Advisory{ID: advLib, Severity: model.SeverityHigh, Modified: day(9), Affected: tt.affected}
			iv := interval(serverID, serverSlug, 11, 101, "go.mod", libPURL, "v1.2.0", model.DirectYes, day(2), time.Time{})
			fs, svc := presetHarness(t, []model.RepoInterval{iv}, a)
			if _, err := svc.Run(t.Context(), model.Scope{}); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			ms := fs.matches[serverID]
			if len(ms) != 1 {
				t.Fatalf("matches = %+v, want one match for the advisory", ms)
			}
			if ms[0].Confidence != tt.wantConf || ms[0].FixedVersion != tt.wantFixed {
				t.Errorf("match confidence %q fixed %q (%s), want %q and %q",
					ms[0].Confidence, ms[0].FixedVersion, ms[0].ConfidenceReason, tt.wantConf, tt.wantFixed)
			}
		})
	}
}

// checkPublishedBetweenRunsFetched publishes an advisory for the server's lib
// v1.2.0 after h's first run, runs again and expects the second run's delta,
// read from the first run's start, to fetch it and raise its finding without
// querying the already queried pairs again.
func checkPublishedBetweenRunsFetched(t *testing.T, h *harness) {
	t.Helper()
	if got, want := h.store.kv[cursorKV], "2026-04-02T00:10:00Z"; got != want {
		t.Fatalf("kv[%s] = %q after the first run, want %q (the run's start)", cursorKV, got, want)
	}
	const late = "GO-2026-0500"
	h.src.records[late] = goRecord(late, libPURL, "1.2.5", model.SeverityHigh, runAt.Add(time.Hour), runAt.Add(time.Hour))
	h.src.prepend("Go", model.AdvisoryRef{ID: late, Modified: runAt.Add(time.Hour)})
	h.src.resetCalls()
	h.now = runAt.Add(6 * time.Hour)
	stats := h.run(t, model.Scope{})

	if len(h.src.queryCalls) != 0 {
		t.Errorf("QueryBatch calls = %v, want none (the pairs were queried by the first run)", h.src.queryCalls)
	}
	if want := []modCall{{ecosystem: "Go", since: runAt}}; !reflect.DeepEqual(h.src.modCalls, want) {
		t.Errorf("second run ModifiedSince calls = %v, want %v", h.src.modCalls, want)
	}
	if _, ok := h.store.advisories[late]; !ok {
		t.Errorf("%s not saved by the second run's delta", late)
	}
	if _, ok := h.store.finding(fingerprint(serverSlug, libPURL, late)); !ok {
		t.Errorf("no finding for %s on %s", late, serverSlug)
	}
	checkStats(t, stats, map[string]int64{"delta_refs": 1, "fetched": 1, "advisories_saved": 1})
}

func TestRun_FirstCursorAtRunStartWithEmptyDelta(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil, nil, vuln.Options{})
	h.src.modified["Go"] = nil
	h.run(t, model.Scope{})
	checkPublishedBetweenRunsFetched(t, h)
}

func TestRun_FirstCursorAtRunStartWhenDeltaWouldFail(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil, nil, vuln.Options{})
	h.src.modErr["Go"] = errors.New("modified_id.csv: status 503")
	if _, err := h.svc.Run(t.Context(), model.Scope{}); err != nil {
		t.Fatalf("first Run() error = %v, want nil (the delta is not read without a cursor)", err)
	}
	if len(h.src.modCalls) != 0 {
		t.Errorf("first run ModifiedSince calls = %v, want none", h.src.modCalls)
	}
	delete(h.src.modErr, "Go")
	checkPublishedBetweenRunsFetched(t, h)
}

func TestRun_EcosystemsSortedWithExtras(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil, nil, vuln.Options{EcosystemsExtra: []string{"npm", "PyPI", "Go", ""}})
	h.run(t, model.Scope{})
	for _, eco := range []string{"Go", "PyPI", "npm"} {
		if got := h.store.kv["osv.modified."+eco]; got != "2026-04-02T00:10:00Z" {
			t.Errorf("kv[osv.modified.%s] = %q after the first run, want the run's start", eco, got)
		}
	}
	if v, ok := h.store.kv["osv.modified."]; ok {
		t.Errorf("cursor stored for the empty ecosystem: %q", v)
	}
	// The first run sets the cursors without reading any delta; the second
	// reads each ecosystem's.
	h.src.resetCalls()
	h.now = runAt.Add(6 * time.Hour)
	h.run(t, model.Scope{})
	var got []string
	for _, c := range h.src.modCalls {
		got = append(got, c.ecosystem)
	}
	if want := []string{"Go", "PyPI", "npm"}; !slices.Equal(got, want) {
		t.Errorf("ModifiedSince ecosystems = %v, want %v (sorted, once each)", got, want)
	}
}

func TestRun_FetchSetKeepsNewestModified(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		queryModified time.Time
		deltaModified time.Time
	}{
		// After the first run advLib is stored at at(10) and the Go delta
		// cursor is at(0). In each case only one of the two refs is after the
		// stored time (both are after the cursor, so the delta returns its
		// line), so the fetch happens only if the union keeps the newer of
		// the two, whichever of them arrives first.
		{"delta newer than the query ref", at(10), at(48)},
		{"query ref newer than the delta", at(48), at(5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil, nil, vuln.Options{})
			stored := h.src.records[advLib]
			stored.Modified = at(10)
			h.src.records[advLib] = stored
			v120 := model.PackageVersion{PURL: libPURL, Version: "v1.2.0"}
			for i, r := range h.src.queryRefs[v120] {
				if r.ID == advLib {
					h.src.queryRefs[v120][i].Modified = at(10)
				}
			}
			h.run(t, model.Scope{})
			if got := h.store.advisories[advLib].Modified; !got.Equal(at(10)) {
				t.Fatalf("stored %s modified = %v after the first run, want %v", advLib, got, at(10))
			}
			// The cursor is at(0), as an earlier run left it.
			h.store.kv[cursorKV] = at(0).Format(time.RFC3339)
			h.src.resetCalls()

			changed := h.src.records[advLib]
			changed.Modified = at(48)
			h.src.records[advLib] = changed
			// A new pair whose query names advLib, and a delta line for it.
			h.store.intervals = append(h.store.intervals,
				interval(serverID, serverSlug, 14, 101, "tools/go.mod", libPURL, "v1.2.1", model.DirectYes, day(8), time.Time{}))
			h.src.queryRefs[model.PackageVersion{PURL: libPURL, Version: "v1.2.1"}] = []model.AdvisoryRef{{ID: advLib, Modified: tt.queryModified}}
			h.src.prepend("Go", model.AdvisoryRef{ID: advLib, Modified: tt.deltaModified})
			h.now = runAt.Add(6 * time.Hour)
			h.run(t, model.Scope{})

			if want := []string{advLib}; !reflect.DeepEqual(h.src.fetchCalls, want) {
				t.Errorf("Fetch calls = %v, want %v (one fetch, the newest time is after the stored one)", h.src.fetchCalls, want)
			}
		})
	}
}

func TestRun_DeltaRecordForAnotherRepoSavedUnderScope(t *testing.T) {
	t.Parallel()
	const adminOnly = "pkg:golang/github.com/example-org/admin-only"
	h := newHarness(t, nil, nil, vuln.Options{})
	h.store.intervals = append(h.store.intervals,
		interval(adminID, adminSlug, 25, 105, "go.mod", adminOnly, "v0.3.0", model.DirectYes, day(2), time.Time{}))
	h.run(t, model.Scope{})
	h.src.resetCalls()

	rec := goRecord("GO-2026-0400", adminOnly, "0.4.0", model.SeverityLow, day(0), at(30))
	h.src.records[rec.ID] = rec
	h.src.prepend("Go", model.AdvisoryRef{ID: rec.ID, Modified: at(30)})
	h.now = runAt.Add(6 * time.Hour)
	stats := h.run(t, model.Scope{RepoSlug: serverSlug})

	if _, ok := h.store.advisories[rec.ID]; !ok {
		t.Errorf("delta record for a package the admin repo uses was not saved under a server-only scope")
	}
	checkStats(t, stats, map[string]int64{"delta_refs": 1, "fetched": 1, "advisories_saved": 1, "delta_irrelevant": 0})
}

func TestRun_SignalsEveryBoundary(t *testing.T) {
	t.Parallel()
	kev := &fakeFeed{signals: []model.Signal{{Kind: model.SignalKEV, CVE: toolCVE}}}
	h := newHarness(t, kev, nil, vuln.Options{SignalsEvery: time.Hour})
	steps := []struct {
		after     time.Duration
		wantCalls int
	}{
		{0, 1},
		{time.Hour, 1},   // exactly the gap is not older than it
		{time.Second, 2}, // now one hour and a second after the refresh
		{59 * time.Minute, 2},
	}
	for i, st := range steps {
		h.now = h.now.Add(st.after)
		h.run(t, model.Scope{})
		if kev.calls != st.wantCalls {
			t.Errorf("run %d at %v: KEV Refresh calls = %d, want %d", i+1, h.now, kev.calls, st.wantCalls)
		}
	}
}

func TestRun_LogsNoAdvisoryText(t *testing.T) {
	t.Parallel()
	const summary = "summary-text-that-must-not-be-logged"
	const details = "details-text-that-must-not-be-logged"
	src := fixtureSource()
	for id, a := range src.records {
		a.Summary, a.Details = summary, details
		src.records[id] = a
	}
	src.fetchErr[advLibB] = errors.New("GET /v1/vulns: status 503")
	var logs lockedBuffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	fs := newFakeStore(fixtureIntervals()...)
	svc := vuln.NewService(src, nil, nil, fs, fs, fs, vuln.Options{Now: func() time.Time { return runAt }}, logger)

	if _, err := svc.Run(t.Context(), model.Scope{}); err == nil {
		t.Fatal("Run() error = nil, want the fetch failure")
	}
	out := logs.String()
	if strings.Contains(out, summary) || strings.Contains(out, details) {
		t.Errorf("logs contain advisory text:\n%s", out)
	}
	if n := strings.Count(out, "level=INFO"); n != 1 {
		t.Errorf("info lines = %d, want 1 per run:\n%s", n, out)
	}
	if !strings.Contains(out, "findings_upserted=") {
		t.Errorf("info line lacks the main counts:\n%s", out)
	}
}
