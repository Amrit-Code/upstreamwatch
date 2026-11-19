package store

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

var (
	pkgLibA = model.Package{PURL: "pkg:golang/github.com/example-org/lib-a", Ecosystem: "golang", Name: "github.com/example-org/lib-a"}
	pkgLibB = model.Package{PURL: "pkg:golang/github.com/example-org/lib-b", Ecosystem: "golang", Name: "github.com/example-org/lib-b"}
	pkgPad  = model.Package{PURL: "pkg:npm/left-pad", Ecosystem: "npm", Name: "left-pad"}

	goMod    = model.ManifestInfo{Ecosystem: "golang", Kind: model.ManifestKindManifest}
	npmLock  = model.ManifestInfo{Ecosystem: "npm", Kind: model.ManifestKindLockfile}
	lockPath = "web/package-lock.json"
)

// sha returns a fixed, distinct 40-character commit SHA for n.
func sha(n int) string { return fmt.Sprintf("%040x", n) }

// day returns t0 plus n days.
func day(n int) time.Time { return t0.Add(time.Duration(n) * 24 * time.Hour) }

// commitAt builds the commit numbered n, committed on day n.
func commitAt(n int) model.Commit {
	return model.Commit{
		SHA:         sha(n),
		CommittedAt: day(n),
		AuthorName:  "Example Author",
		AuthorEmail: "author@example.com",
		Subject:     fmt.Sprintf("commit %d", n),
		Paths:       []string{"go.mod"},
	}
}

// newTimelineRepo stores a repo through the store and returns its ID.
func newTimelineRepo(t *testing.T, db *DB) int64 {
	t.Helper()
	r, err := db.UpsertRepo(t.Context(), "example-server", repoURL)
	if err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	return r.ID
}

// apply runs ApplyTimeline and fails the test on error.
func apply(t *testing.T, db *DB, u model.TimelineUpdate) {
	t.Helper()
	if err := db.ApplyTimeline(t.Context(), u); err != nil {
		t.Fatalf("ApplyTimeline() error = %v", err)
	}
}

// positions returns the repo's commit SHAs mapped to their positions.
func positions(t *testing.T, db *DB, repoID int64) map[string]int64 {
	t.Helper()
	rows, err := db.pool.QueryContext(t.Context(), `SELECT sha, position FROM commits WHERE repo_id = ?`, repoID)
	if err != nil {
		t.Fatalf("read positions: %v", err)
	}
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() {
		var s string
		var p int64
		if err := rows.Scan(&s, &p); err != nil {
			t.Fatalf("read positions: %v", err)
		}
		got[s] = p
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read positions: %v", err)
	}
	return got
}

// listIntervals calls ListIntervals and fails the test on error.
func listIntervals(t *testing.T, db *DB, repoID int64) []model.DepInterval {
	t.Helper()
	got, err := db.ListIntervals(t.Context(), repoID)
	if err != nil {
		t.Fatalf("ListIntervals() error = %v", err)
	}
	return got
}

// listEvents calls ListEvents and fails the test on error.
func listEvents(t *testing.T, db *DB, repoID int64) []model.DepEvent {
	t.Helper()
	got, err := db.ListEvents(t.Context(), repoID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	return got
}

// openInterval builds an open interval of p in go.mod from commit n.
func openInterval(p model.Package, version string, n int) model.DepInterval {
	return model.DepInterval{ManifestPath: "go.mod", Package: p, Version: version, Requirement: version,
		Direct: model.DirectYes, FromSHA: sha(n), From: day(n)}
}

// closeAt returns iv closed at commit n.
func closeAt(iv model.DepInterval, n int) model.DepInterval {
	iv.ToSHA, iv.To = sha(n), day(n)
	return iv
}

// withIDs returns ivs with the IDs of got, matched by position, so a
// comparison checks every other field.
func withIDs(ivs []model.DepInterval, got []model.DepInterval) []model.DepInterval {
	out := make([]model.DepInterval, len(ivs))
	for i, iv := range ivs {
		if i < len(got) {
			iv.ID = got[i].ID
		}
		out[i] = iv
	}
	return out
}

func TestDB_ApplyTimeline_RoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID := newTimelineRepo(t, db)

	c1, c2 := commitAt(1), commitAt(2)
	want := []model.DepEvent{
		{CommitSHA: c1.SHA, CommittedAt: c1.CommittedAt, ManifestPath: "go.mod", Package: pkgLibA,
			Change: model.ChangeAdded, Version: "v1.0.0", Requirement: "v1.0.0", Direct: model.DirectYes},
		{CommitSHA: c1.SHA, CommittedAt: c1.CommittedAt, ManifestPath: "go.mod", Package: pkgLibB,
			Change: model.ChangeAdded, Version: "v0.1.0", Requirement: "v0.1.0", Direct: model.DirectNo},
		{CommitSHA: c1.SHA, CommittedAt: c1.CommittedAt, ManifestPath: lockPath, Package: pkgPad,
			Change: model.ChangeAdded, Version: "1.3.0", Direct: model.DirectUnknown, Scope: "runtime"},
		{CommitSHA: c2.SHA, CommittedAt: c2.CommittedAt, ManifestPath: "go.mod", Package: pkgLibA,
			Change: model.ChangeChanged, Version: "v1.1.0", Requirement: "v1.1.0", Direct: model.DirectYes, PrevVersion: "v1.0.0"},
		{CommitSHA: c2.SHA, CommittedAt: c2.CommittedAt, ManifestPath: "go.mod", Package: pkgLibB,
			Change: model.ChangeRemoved, Version: "v0.1.0", Requirement: "v0.1.0", Direct: model.DirectNo, PrevVersion: "v0.1.0"},
	}
	// Insert out of order so the result proves ListEvents sorts.
	in := []model.DepEvent{want[4], want[2], want[3], want[1], want[0]}
	apply(t, db, model.TimelineUpdate{
		RepoID:     repoID,
		Commits:    []model.Commit{c1, c2},
		Manifests:  map[string]model.ManifestInfo{"go.mod": goMod, lockPath: npmLock},
		Events:     in,
		IndexedSHA: c2.SHA,
	})

	if got := listEvents(t, db, repoID); !reflect.DeepEqual(got, want) {
		t.Errorf("ListEvents() =\n%+v\nwant\n%+v", got, want)
	}
	wantPos := map[string]int64{c1.SHA: 0, c2.SHA: 1}
	if got := positions(t, db, repoID); !reflect.DeepEqual(got, wantPos) {
		t.Errorf("commit positions = %v, want %v", got, wantPos)
	}
	repo, err := db.GetRepo(t.Context(), "example-server")
	if err != nil {
		t.Fatalf("GetRepo() error = %v", err)
	}
	if repo.LastIndexedSHA != c2.SHA {
		t.Errorf("LastIndexedSHA = %q, want %q", repo.LastIndexedSHA, c2.SHA)
	}
	// Directness and empty strings land as the documented column values.
	if n := count(t, db, `SELECT COUNT(*) FROM dep_events WHERE direct IS NULL AND requirement IS NULL AND prev_version IS NULL`); n != 1 {
		t.Errorf("events with NULL direct, requirement and prev_version = %d, want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM dep_events WHERE direct = 0`); n != 2 {
		t.Errorf("events with direct 0 = %d, want 2", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM manifests WHERE repo_id = ? AND path = ? AND ecosystem = 'npm' AND kind = 'lockfile'`, repoID, lockPath); n != 1 {
		t.Errorf("lockfile manifest rows = %d, want 1", n)
	}
}

func TestDB_ApplyTimeline_Incremental(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID := newTimelineRepo(t, db)

	libA := openInterval(pkgLibA, "v1.0.0", 1)
	libB := openInterval(pkgLibB, "v0.1.0", 1)
	apply(t, db, model.TimelineUpdate{
		RepoID:    repoID,
		Commits:   []model.Commit{commitAt(1), commitAt(2)},
		Manifests: map[string]model.ManifestInfo{"go.mod": goMod},
		Events: []model.DepEvent{
			{CommitSHA: sha(1), ManifestPath: "go.mod", Package: pkgLibA, Change: model.ChangeAdded, Version: "v1.0.0"},
			{CommitSHA: sha(1), ManifestPath: "go.mod", Package: pkgLibB, Change: model.ChangeAdded, Version: "v0.1.0"},
		},
		Intervals:  []model.DepInterval{libA, libB},
		IndexedSHA: sha(2),
	})
	before := listIntervals(t, db, repoID)
	if len(before) != 2 {
		t.Fatalf("intervals after first update = %+v, want 2", before)
	}

	libBClosed := closeAt(libB, 3)
	libBNew := openInterval(pkgLibB, "v0.2.0", 3)
	apply(t, db, model.TimelineUpdate{
		RepoID:  repoID,
		Commits: []model.Commit{commitAt(3), commitAt(4)},
		// go.mod is already stored, so the second update need not repeat it.
		Events: []model.DepEvent{
			{CommitSHA: sha(3), ManifestPath: "go.mod", Package: pkgLibB, Change: model.ChangeChanged,
				Version: "v0.2.0", PrevVersion: "v0.1.0"},
		},
		Intervals:  []model.DepInterval{libBNew, libA, libBClosed},
		IndexedSHA: sha(4),
	})

	wantPos := map[string]int64{sha(1): 0, sha(2): 1, sha(3): 2, sha(4): 3}
	if got := positions(t, db, repoID); !reflect.DeepEqual(got, wantPos) {
		t.Errorf("commit positions = %v, want %v", got, wantPos)
	}
	got := listIntervals(t, db, repoID)
	libA.ID, libBClosed.ID = before[0].ID, before[1].ID
	want := []model.DepInterval{libA, libBClosed, libBNew}
	if len(got) == 3 {
		want[2].ID = got[2].ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListIntervals() =\n%+v\nwant\n%+v", got, want)
	}
	if len(got) == 3 && (got[2].ID == before[0].ID || got[2].ID == before[1].ID) {
		t.Errorf("new interval reused ID %d", got[2].ID)
	}
	if n := len(listEvents(t, db, repoID)); n != 3 {
		t.Errorf("events = %d, want 3", n)
	}
}

func TestDB_ApplyTimeline_DeletesVanishedIntervals(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID := newTimelineRepo(t, db)

	libA := openInterval(pkgLibA, "v1.0.0", 1)
	libB := openInterval(pkgLibB, "v0.1.0", 1)
	apply(t, db, model.TimelineUpdate{
		RepoID:     repoID,
		Commits:    []model.Commit{commitAt(1)},
		Manifests:  map[string]model.ManifestInfo{"go.mod": goMod},
		Intervals:  []model.DepInterval{libA, libB},
		IndexedSHA: sha(1),
	})
	before := listIntervals(t, db, repoID)

	apply(t, db, model.TimelineUpdate{RepoID: repoID, Intervals: []model.DepInterval{libA}, IndexedSHA: sha(1)})

	libA.ID = before[0].ID
	if got := listIntervals(t, db, repoID); !reflect.DeepEqual(got, []model.DepInterval{libA}) {
		t.Errorf("ListIntervals() = %+v, want only %+v", got, libA)
	}

	apply(t, db, model.TimelineUpdate{RepoID: repoID, IndexedSHA: sha(1)})
	if got := listIntervals(t, db, repoID); got != nil {
		t.Errorf("ListIntervals() after an empty set = %+v, want nil", got)
	}
}

func TestDB_ApplyTimeline_Reset(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID := newTimelineRepo(t, db)

	apply(t, db, model.TimelineUpdate{
		RepoID:    repoID,
		Commits:   []model.Commit{commitAt(1), commitAt(2)},
		Manifests: map[string]model.ManifestInfo{"go.mod": goMod, lockPath: npmLock},
		Events: []model.DepEvent{
			{CommitSHA: sha(1), ManifestPath: "go.mod", Package: pkgLibA, Change: model.ChangeAdded, Version: "v1.0.0"},
			{CommitSHA: sha(2), ManifestPath: lockPath, Package: pkgPad, Change: model.ChangeAdded, Version: "1.3.0"},
		},
		Intervals:  []model.DepInterval{openInterval(pkgLibA, "v1.0.0", 1)},
		IndexedSHA: sha(2),
	})
	old := listIntervals(t, db, repoID)
	if len(old) != 1 {
		t.Fatalf("intervals before reset = %+v, want 1", old)
	}
	exec(t, db, `INSERT INTO advisories (id, created_ts, updated_ts) VALUES ('GO-2026-0001', ?, ?)`, ts0, ts0)
	exec(t, db, `INSERT INTO advisory_matches (repo_id, dep_version_id, advisory_id, created_ts, updated_ts)
		VALUES (?, ?, 'GO-2026-0001', ?, ?)`, repoID, old[0].ID, ts0, ts0)
	exec(t, db, `INSERT INTO findings (kind, repo_id, dep_version_id, advisory_id, fingerprint, score,
		rank_reason, first_seen_ts, last_changed_ts, created_ts, updated_ts)
		VALUES ('advisory', ?, ?, 'GO-2026-0001', 'advisory|reset', 5, 'r', ?, ?, ?, ?)`,
		repoID, old[0].ID, ts0, ts0, ts0, ts0)

	rewritten := commitAt(10)
	newEvent := model.DepEvent{CommitSHA: rewritten.SHA, CommittedAt: rewritten.CommittedAt, ManifestPath: "go.mod",
		Package: pkgLibA, Change: model.ChangeAdded, Version: "v1.2.0"}
	newInterval := openInterval(pkgLibA, "v1.2.0", 10)
	apply(t, db, model.TimelineUpdate{
		RepoID:     repoID,
		Reset:      true,
		Commits:    []model.Commit{rewritten},
		Manifests:  map[string]model.ManifestInfo{"go.mod": goMod},
		Events:     []model.DepEvent{newEvent},
		Intervals:  []model.DepInterval{newInterval},
		IndexedSHA: rewritten.SHA,
	})

	if got := positions(t, db, repoID); !reflect.DeepEqual(got, map[string]int64{rewritten.SHA: 0}) {
		t.Errorf("commits after reset = %v, want only %s at 0", got, rewritten.SHA)
	}
	if got := listEvents(t, db, repoID); !reflect.DeepEqual(got, []model.DepEvent{newEvent}) {
		t.Errorf("ListEvents() after reset = %+v, want %+v", got, newEvent)
	}
	got := listIntervals(t, db, repoID)
	if want := withIDs([]model.DepInterval{newInterval}, got); !reflect.DeepEqual(got, want) {
		t.Errorf("ListIntervals() after reset = %+v, want %+v", got, want)
	}
	// SQLite may reuse the old row ID for the new interval (no AUTOINCREMENT),
	// so the old row's absence shows through the cascade and the commits.
	if n := count(t, db, `SELECT COUNT(*) FROM dep_versions WHERE repo_id = ?`, repoID); n != 1 {
		t.Errorf("interval rows after reset = %d, want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM advisory_matches`); n != 0 {
		t.Errorf("advisory_matches rows = %d, want 0 (cascade)", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM findings WHERE fingerprint = 'advisory|reset' AND dep_version_id IS NULL`); n != 1 {
		t.Errorf("findings kept with NULL dep_version_id = %d, want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM manifests WHERE repo_id = ?`, repoID); n != 2 {
		t.Errorf("manifests after reset = %d, want 2", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM packages`); n != 2 {
		t.Errorf("packages after reset = %d, want 2", n)
	}
}

// timelineSnapshot is everything an ApplyTimeline call can change for a repo.
type timelineSnapshot struct {
	repo      model.TargetRepo
	positions map[string]int64
	events    []model.DepEvent
	intervals []model.DepInterval
	manifests int
	packages  int
}

func snapshot(t *testing.T, db *DB, repoID int64) timelineSnapshot {
	t.Helper()
	repo, err := db.GetRepo(t.Context(), "example-server")
	if err != nil {
		t.Fatalf("GetRepo() error = %v", err)
	}
	return timelineSnapshot{
		repo:      repo,
		positions: positions(t, db, repoID),
		events:    listEvents(t, db, repoID),
		intervals: listIntervals(t, db, repoID),
		manifests: count(t, db, `SELECT COUNT(*) FROM manifests`),
		packages:  count(t, db, `SELECT COUNT(*) FROM packages`),
	}
}

func TestDB_ApplyTimeline_AtomicOnError(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID := newTimelineRepo(t, db)

	libA := openInterval(pkgLibA, "v1.0.0", 1)
	apply(t, db, model.TimelineUpdate{
		RepoID:    repoID,
		Commits:   []model.Commit{commitAt(1)},
		Manifests: map[string]model.ManifestInfo{"go.mod": goMod},
		Events: []model.DepEvent{
			{CommitSHA: sha(1), ManifestPath: "go.mod", Package: pkgLibA, Change: model.ChangeAdded, Version: "v1.0.0"},
		},
		Intervals:  []model.DepInterval{libA},
		IndexedSHA: sha(1),
	})
	before := snapshot(t, db, repoID)

	err := db.ApplyTimeline(t.Context(), model.TimelineUpdate{
		RepoID:    repoID,
		Commits:   []model.Commit{commitAt(2)},
		Manifests: map[string]model.ManifestInfo{lockPath: npmLock},
		Events: []model.DepEvent{
			{CommitSHA: sha(2), ManifestPath: lockPath, Package: pkgPad, Change: model.ChangeAdded, Version: "1.3.0"},
			{CommitSHA: sha(2), ManifestPath: "missing/go.mod", Package: pkgLibB, Change: model.ChangeAdded, Version: "v0.1.0"},
		},
		Intervals:  []model.DepInterval{closeAt(libA, 2), openInterval(pkgLibA, "v1.1.0", 2)},
		IndexedSHA: sha(2),
	})
	if err == nil || !strings.Contains(err.Error(), "missing/go.mod") {
		t.Fatalf("ApplyTimeline() error = %v, want one naming missing/go.mod", err)
	}
	if after := snapshot(t, db, repoID); !reflect.DeepEqual(after, before) {
		t.Errorf("database changed by a failed update:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestDB_ListIntervals_Order(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID := newTimelineRepo(t, db)

	if got := listIntervals(t, db, repoID); got != nil {
		t.Fatalf("ListIntervals() on an empty repo = %+v, want nil", got)
	}

	inAPI := openInterval(pkgLibA, "v1.0.0", 1)
	inAPI.ManifestPath = "api/go.mod"
	inAPI.Direct = model.DirectUnknown
	inAPI.Requirement = ""
	want := []model.DepInterval{
		inAPI,
		closeAt(openInterval(pkgLibA, "v1.0.0", 1), 2),
		openInterval(pkgLibA, "v1.1.0", 2),
		func() model.DepInterval {
			iv := openInterval(pkgLibB, "v0.1.0", 1)
			iv.Direct = model.DirectNo
			return iv
		}(),
	}
	apply(t, db, model.TimelineUpdate{
		RepoID:     repoID,
		Commits:    []model.Commit{commitAt(1), commitAt(2)},
		Manifests:  map[string]model.ManifestInfo{"go.mod": goMod, "api/go.mod": goMod},
		Intervals:  []model.DepInterval{want[3], want[2], want[1], want[0]},
		IndexedSHA: sha(2),
	})

	got := listIntervals(t, db, repoID)
	if want := withIDs(want, got); !reflect.DeepEqual(got, want) {
		t.Errorf("ListIntervals() =\n%+v\nwant\n%+v", got, want)
	}
	for _, iv := range got {
		if iv.ID == 0 {
			t.Errorf("interval %+v has no ID", iv)
		}
	}
}

func TestDB_ApplyTimeline_UnchangedUpdateWritesNothing(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID := newTimelineRepo(t, db)
	u := model.TimelineUpdate{
		RepoID:     repoID,
		Commits:    []model.Commit{commitAt(1)},
		Manifests:  map[string]model.ManifestInfo{"go.mod": goMod},
		Intervals:  []model.DepInterval{openInterval(pkgLibA, "v1.0.0", 1)},
		IndexedSHA: sha(1),
	}
	apply(t, db, u)

	db.now = func() time.Time { return t0.Add(time.Hour) }
	u.Commits = nil
	apply(t, db, u)

	for _, table := range []string{"repos", "commits", "manifests", "packages", "dep_versions"} {
		if n := count(t, db, `SELECT COUNT(*) FROM `+table+` WHERE updated_ts <> ?`, ts0); n != 0 {
			t.Errorf("%s rows rewritten by an unchanged update = %d, want 0", table, n)
		}
	}
}

func TestDB_ApplyTimeline_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		update  func(repoID int64) model.TimelineUpdate
		wantErr string
		wantIs  error
	}{
		{
			name:   "unknown repo",
			update: func(int64) model.TimelineUpdate { return model.TimelineUpdate{RepoID: 999, IndexedSHA: sha(1)} },
			wantIs: model.ErrNotFound,
		},
		{
			name: "event commit not stored",
			update: func(id int64) model.TimelineUpdate {
				return model.TimelineUpdate{RepoID: id, Manifests: map[string]model.ManifestInfo{"go.mod": goMod},
					Events: []model.DepEvent{{CommitSHA: sha(7), ManifestPath: "go.mod", Package: pkgLibA, Change: model.ChangeAdded}}}
			},
			wantErr: sha(7),
		},
		{
			name: "interval closing commit not stored",
			update: func(id int64) model.TimelineUpdate {
				return model.TimelineUpdate{RepoID: id, Commits: []model.Commit{commitAt(1)},
					Manifests: map[string]model.ManifestInfo{"go.mod": goMod},
					Intervals: []model.DepInterval{closeAt(openInterval(pkgLibA, "v1.0.0", 1), 8)}}
			},
			wantErr: sha(8),
		},
		{
			name: "interval manifest not stored",
			update: func(id int64) model.TimelineUpdate {
				iv := openInterval(pkgLibA, "v1.0.0", 1)
				iv.ManifestPath = "tools/go.mod"
				return model.TimelineUpdate{RepoID: id, Commits: []model.Commit{commitAt(1)}, Intervals: []model.DepInterval{iv}}
			},
			wantErr: "tools/go.mod",
		},
		{
			name: "duplicate commit",
			update: func(id int64) model.TimelineUpdate {
				return model.TimelineUpdate{RepoID: id, Commits: []model.Commit{commitAt(1), commitAt(1)}}
			},
			wantErr: sha(1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			repoID := newTimelineRepo(t, db)
			err := db.ApplyTimeline(t.Context(), tt.update(repoID))
			if err == nil {
				t.Fatal("ApplyTimeline() error = nil, want an error")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("ApplyTimeline() error = %v, want %v", err, tt.wantIs)
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ApplyTimeline() error = %v, want it to name %q", err, tt.wantErr)
			}
			if n := count(t, db, `SELECT COUNT(*) FROM commits`); n != 0 {
				t.Errorf("commits after a failed update = %d, want 0", n)
			}
		})
	}
}

// packageRow reads the stored ecosystem, name and updated_ts of purl.
func packageRow(t *testing.T, db *DB, purl string) (ecosystem, name, updated string) {
	t.Helper()
	var eco, nm sql.NullString
	if err := db.pool.QueryRowContext(t.Context(),
		`SELECT ecosystem, name, updated_ts FROM packages WHERE purl = ?`, purl).Scan(&eco, &nm, &updated); err != nil {
		t.Fatalf("read package %q: %v", purl, err)
	}
	return eco.String, nm.String, updated
}

// packageUpdate is a timeline update of the repo holding one open interval of
// p from commit 1.
func packageUpdate(repoID int64, p model.Package) model.TimelineUpdate {
	return model.TimelineUpdate{
		RepoID:     repoID,
		Manifests:  map[string]model.ManifestInfo{"go.mod": goMod},
		Intervals:  []model.DepInterval{openInterval(p, "3.1.2", 1)},
		IndexedSHA: sha(1),
	}
}

func TestDB_ApplyTimeline_PackageKeepsFirstSpelling(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID := newTimelineRepo(t, db)
	const purl = "pkg:pypi/jinja2"

	first := packageUpdate(repoID, model.Package{PURL: purl, Ecosystem: "pypi", Name: "Jinja2"})
	first.Commits = []model.Commit{commitAt(1)}
	apply(t, db, first)
	db.now = func() time.Time { return t0.Add(time.Hour) }
	apply(t, db, packageUpdate(repoID, model.Package{PURL: purl, Ecosystem: "pypi", Name: "jinja2"}))

	eco, name, updated := packageRow(t, db, purl)
	if name != "Jinja2" {
		t.Errorf("packages.name after a respelled upsert = %q, want %q", name, "Jinja2")
	}
	if eco != "pypi" {
		t.Errorf("packages.ecosystem = %q, want %q", eco, "pypi")
	}
	if updated != ts0 {
		t.Errorf("packages.updated_ts after a respelled upsert = %q, want %q (unchanged)", updated, ts0)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM packages WHERE purl = ?`, purl); n != 1 {
		t.Errorf("packages rows for %q = %d, want 1", purl, n)
	}
}

func TestDB_ApplyTimeline_PackageGainsOnlyAMissingEcosystem(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID := newTimelineRepo(t, db)
	const purl = "pkg:pypi/requests"

	first := packageUpdate(repoID, model.Package{PURL: purl, Name: "Requests"})
	first.Commits = []model.Commit{commitAt(1)}
	apply(t, db, first)
	later := t0.Add(time.Hour)
	db.now = func() time.Time { return later }
	apply(t, db, packageUpdate(repoID, model.Package{PURL: purl, Ecosystem: "pypi", Name: "requests"}))

	eco, name, updated := packageRow(t, db, purl)
	if eco != "pypi" || name != "Requests" {
		t.Errorf("package = ecosystem %q name %q, want %q %q", eco, name, "pypi", "Requests")
	}
	if want := later.Format(time.RFC3339); updated != want {
		t.Errorf("packages.updated_ts after gaining an ecosystem = %q, want %q", updated, want)
	}

	// An ecosystem already set is not replaced.
	db.now = func() time.Time { return later.Add(time.Hour) }
	apply(t, db, packageUpdate(repoID, model.Package{PURL: purl, Ecosystem: "other", Name: "requests"}))
	if eco, _, updated := packageRow(t, db, purl); eco != "pypi" || updated != later.Format(time.RFC3339) {
		t.Errorf("package after another ecosystem = %q updated %q, want %q updated %q",
			eco, updated, "pypi", later.Format(time.RFC3339))
	}
}
