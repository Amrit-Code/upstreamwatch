package vuln_test

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
)

// These tests run vuln.Service over a real SQLite file, opened and migrated
// as start-up does it, with the advisory database faked.

// openDB opens and migrates a fresh database file.
func openDB(t *testing.T) *store.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upw.sqlite")
	db, err := store.Open(t.Context(), path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("store.Open(%s) error = %v", path, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return db
}

// seedServer stores the example-server repo with two commits and lib v1.2.0
// pinned in go.mod since the second.
func seedServer(t *testing.T, db *store.DB) {
	t.Helper()
	r, err := db.UpsertRepo(t.Context(), serverSlug, "https://github.com/example-org/example-server")
	if err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	commit := func(n int) model.Commit {
		return model.Commit{
			SHA: fmt.Sprintf("%040x", n), CommittedAt: day(n), AuthorName: "Example Author",
			AuthorEmail: "author@example.com", Subject: fmt.Sprintf("commit %d", n), Paths: []string{"go.mod"},
		}
	}
	err = db.ApplyTimeline(t.Context(), model.TimelineUpdate{
		RepoID:    r.ID,
		Commits:   []model.Commit{commit(1), commit(2)},
		Manifests: map[string]model.ManifestInfo{"go.mod": {Ecosystem: "golang", Kind: model.ManifestKindManifest}},
		Intervals: []model.DepInterval{{
			ManifestPath: "go.mod",
			Package:      model.Package{PURL: libPURL, Ecosystem: "golang", Name: "github.com/example-org/lib"},
			Version:      "v1.2.0",
			Requirement:  "v1.2.0",
			Direct:       model.DirectYes,
			FromSHA:      commit(2).SHA,
			From:         day(2),
		}},
		IndexedSHA: commit(2).SHA,
	})
	if err != nil {
		t.Fatalf("ApplyTimeline() error = %v", err)
	}
}

func TestRun_FindingStableAcrossRuns(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	seedServer(t, db)

	src := newFakeSource()
	lib := goRecord(advLib, libPURL, "1.2.5", model.SeverityHigh, day(3), day(9))
	src.records[advLib] = lib
	src.queryRefs[model.PackageVersion{PURL: libPURL, Version: "v1.2.0"}] = []model.AdvisoryRef{{ID: advLib, Modified: day(9)}}
	src.modified["Go"] = []model.AdvisoryRef{{ID: "GO-2026-0100", Modified: at(0)}}

	now := runAt
	svc := vuln.NewService(src, nil, nil, db, db, db,
		vuln.Options{Now: func() time.Time { return now }}, slog.New(slog.DiscardHandler))

	first, err := svc.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	checkStats(t, first, map[string]int64{"pairs_new": 1, "fetched": 1, "advisories_saved": 1, "matches": 1, "findings_upserted": 1})
	before := unresolved(t, db)

	now = runAt.Add(6 * time.Hour)
	second, err := svc.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	checkStats(t, second, map[string]int64{"pairs_new": 0, "fetched": 0, "matches": 1, "findings_upserted": 1, "findings_resolved": 0})
	after := unresolved(t, db)

	if after.ID != before.ID {
		t.Errorf("finding ID = %d after the second run, want %d", after.ID, before.ID)
	}
	if !after.LastChangedAt.Equal(runAt) || !before.LastChangedAt.Equal(runAt) {
		t.Errorf("LastChangedAt = %v then %v, want %v both times", before.LastChangedAt, after.LastChangedAt, runAt)
	}
	if want := fingerprint(serverSlug, libPURL, advLib); after.Fingerprint != want {
		t.Errorf("Fingerprint = %q, want %q", after.Fingerprint, want)
	}
	if after.Score != 100 || after.RankReason != "high 70; direct +10; exposed now +20; fixed in 1.2.5" {
		t.Errorf("score %v, reason %q, want 100 and the high/direct/exposed reason", after.Score, after.RankReason)
	}
	if cursor, err := db.GetKV(t.Context(), cursorKV); err != nil || cursor != "2026-04-02T00:10:00Z" {
		t.Errorf("GetKV(%s) = %q, %v, want the first run's start", cursorKV, cursor, err)
	}
}

// unresolved returns the one unresolved advisory finding.
func unresolved(t *testing.T, db *store.DB) model.Finding {
	t.Helper()
	fs, err := db.ListUnresolvedFindings(t.Context(), model.KindAdvisory)
	if err != nil {
		t.Fatalf("ListUnresolvedFindings() error = %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("unresolved advisory findings = %d, want 1: %+v", len(fs), fs)
	}
	return fs[0]
}
