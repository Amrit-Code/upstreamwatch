package store

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const ts0 = "2026-03-01T12:00:00Z"

// seedAdvisoryParents inserts a repo, a package and an advisory with raw SQL,
// since no store method writes those tables yet, and returns their keys.
func seedAdvisoryParents(t *testing.T, db *DB) (repoID, packageID int64, advisoryID string) {
	t.Helper()
	exec(t, db, `INSERT INTO repos (id, slug, url, created_ts, updated_ts)
		VALUES (1, 'example-server', 'https://github.com/example-org/example-server', ?, ?)`, ts0, ts0)
	exec(t, db, `INSERT INTO packages (id, purl, ecosystem, name, created_ts, updated_ts)
		VALUES (2, 'pkg:golang/github.com/example-org/example-lib', 'golang', 'github.com/example-org/example-lib', ?, ?)`, ts0, ts0)
	exec(t, db, `INSERT INTO advisories (id, created_ts, updated_ts) VALUES ('GO-2026-0001', ?, ?)`, ts0, ts0)
	return 1, 2, "GO-2026-0001"
}

func TestDB_UpsertFinding_InsertThenUpdate(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repoID, packageID, advisoryID := seedAdvisoryParents(t, db)

	in := model.Finding{
		Kind:          model.KindAdvisory,
		RepoID:        repoID,
		PackageID:     packageID,
		AdvisoryID:    advisoryID,
		Fingerprint:   "advisory|1|2|GO-2026-0001",
		Score:         7.5,
		RankReason:    "high severity",
		FirstSeenAt:   t0.Add(-48 * time.Hour), // ignored: first-seen comes from LastChangedAt
		LastChangedAt: t0,
	}
	first, err := db.UpsertFinding(ctx, in)
	if err != nil {
		t.Fatalf("first UpsertFinding() error = %v", err)
	}
	want := in
	want.ID = first.ID
	want.FirstSeenAt = t0
	want.State = model.FindingState{State: model.StateOpen}
	if first.ID == 0 || !reflect.DeepEqual(first, want) {
		t.Errorf("first UpsertFinding() = %+v, want %+v", first, want)
	}

	// Simulate what later milestones do: the operator acks it, a digest
	// reports it, and the pin leaves the range.
	exec(t, db, `UPDATE finding_state SET state = 'acked', note = 'seen', changed_ts = ?, changed_by = 'operator' WHERE finding_id = ?`,
		"2026-03-01T13:00:00Z", first.ID)
	exec(t, db, `UPDATE findings SET reported_in_digest_ts = ? WHERE id = ?`, "2026-03-01T14:00:00Z", first.ID)
	if err := db.ResolveFinding(ctx, in.Fingerprint, t0.Add(2*time.Hour)); err != nil {
		t.Fatalf("ResolveFinding() error = %v", err)
	}

	again := in
	again.Score = 9.1
	again.RankReason = "in KEV"
	again.LastChangedAt = t0.Add(24 * time.Hour)
	second, err := db.UpsertFinding(ctx, again)
	if err != nil {
		t.Fatalf("second UpsertFinding() error = %v", err)
	}
	want = in
	want.ID = first.ID
	want.Score = 9.1
	want.RankReason = "in KEV"
	want.FirstSeenAt = t0
	want.LastChangedAt = t0.Add(24 * time.Hour)
	want.ReportedInDigestAt = time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)
	want.State = model.FindingState{
		State:     model.StateAcked,
		Note:      "seen",
		ChangedAt: time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC),
		ChangedBy: "operator",
	}
	if !reflect.DeepEqual(second, want) {
		t.Errorf("second UpsertFinding() = %+v, want %+v", second, want)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM findings"); got != 1 {
		t.Errorf("findings rows = %d, want 1", got)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM finding_state"); got != 1 {
		t.Errorf("finding_state rows = %d, want 1", got)
	}
}

func TestDB_UpsertFinding_StoreTimestamps(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	f, err := db.UpsertFinding(t.Context(), model.Finding{Kind: model.KindJobFailure, Fingerprint: "job_failure|issues", LastChangedAt: t0.Add(time.Hour)})
	if err != nil {
		t.Fatalf("UpsertFinding() error = %v", err)
	}
	for _, table := range []string{"findings", "finding_state"} {
		var created, updated string
		col := "id"
		if table == "finding_state" {
			col = "finding_id"
		}
		if err := db.pool.QueryRowContext(t.Context(), "SELECT created_ts, updated_ts FROM "+table+" WHERE "+col+" = ?", f.ID).Scan(&created, &updated); err != nil {
			t.Fatalf("read %s timestamps: %v", table, err)
		}
		if created != ts0 || updated != ts0 {
			t.Errorf("%s created_ts, updated_ts = %s, %s; want the store clock %s", table, created, updated, ts0)
		}
	}
}

func TestDB_UpsertFinding_JobFailureHasNullRefs(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	in := model.Finding{
		Kind:          model.KindJobFailure,
		Fingerprint:   "job_failure|issues",
		Score:         1,
		RankReason:    "job failed",
		LastChangedAt: t0,
	}
	got, err := db.UpsertFinding(t.Context(), in)
	if err != nil {
		t.Fatalf("UpsertFinding() error = %v", err)
	}
	if got.RepoID != 0 || got.PackageID != 0 || got.DepVersionID != 0 || got.AdvisoryID != "" || got.IssueID != 0 || got.VerdictID != 0 {
		t.Errorf("UpsertFinding() refs = %+v, want all zero", got)
	}
	var repo, pkg, dv, issue, verdict sql.NullInt64
	var adv sql.NullString
	if err := db.pool.QueryRowContext(t.Context(),
		"SELECT repo_id, package_id, dep_version_id, advisory_id, issue_id, verdict_id FROM findings WHERE id = ?", got.ID).
		Scan(&repo, &pkg, &dv, &adv, &issue, &verdict); err != nil {
		t.Fatalf("read refs: %v", err)
	}
	if repo.Valid || pkg.Valid || dv.Valid || adv.Valid || issue.Valid || verdict.Valid {
		t.Errorf("stored refs = %v %v %v %v %v %v, want all NULL", repo, pkg, dv, adv, issue, verdict)
	}
}

func TestDB_UpsertFinding_UpdatesOnlyDepVersionLink(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repoID, packageID, advisoryID := seedAdvisoryParents(t, db)
	exec(t, db, `INSERT INTO manifests (id, repo_id, path, created_ts, updated_ts) VALUES (3, ?, 'go.mod', ?, ?)`, repoID, ts0, ts0)
	exec(t, db, `INSERT INTO commits (id, repo_id, sha, created_ts, updated_ts) VALUES (4, ?, 'abc123', ?, ?)`, repoID, ts0, ts0)
	for _, id := range []int64{5, 8} {
		exec(t, db, `INSERT INTO dep_versions (id, repo_id, package_id, manifest_id, version, from_commit_id, created_ts, updated_ts)
			VALUES (?, ?, ?, 3, 'v1.2.0', 4, ?, ?)`, id, repoID, packageID, ts0, ts0)
	}
	const fp = "advisory|1|2|GO-2026-0001"
	first, err := db.UpsertFinding(ctx, model.Finding{
		Kind: model.KindAdvisory, RepoID: repoID, PackageID: packageID, DepVersionID: 5,
		AdvisoryID: advisoryID, Fingerprint: fp, LastChangedAt: t0,
	})
	if err != nil {
		t.Fatalf("first UpsertFinding() error = %v", err)
	}

	tests := []struct {
		name         string
		depVersionID int64
		wantValid    bool
	}{
		{name: "new interval", depVersionID: 8, wantValid: true},
		{name: "zero stores NULL", depVersionID: 0, wantValid: false},
	}
	for _, tt := range tests {
		// The other links are left out of the upsert on purpose: they must
		// keep the values the finding was created with.
		got, err := db.UpsertFinding(ctx, model.Finding{
			Kind: model.KindAdvisory, DepVersionID: tt.depVersionID, Fingerprint: fp, LastChangedAt: t0.Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("%s: UpsertFinding() error = %v", tt.name, err)
		}
		if got.ID != first.ID || got.DepVersionID != tt.depVersionID {
			t.Errorf("%s: UpsertFinding() = ID %d dep_version %d, want ID %d dep_version %d",
				tt.name, got.ID, got.DepVersionID, first.ID, tt.depVersionID)
		}
		if got.RepoID != repoID || got.PackageID != packageID || got.AdvisoryID != advisoryID || !got.FirstSeenAt.Equal(t0) {
			t.Errorf("%s: UpsertFinding() = %+v, want repo, package, advisory and first seen unchanged", tt.name, got)
		}
		var dv sql.NullInt64
		if err := db.pool.QueryRowContext(ctx, "SELECT dep_version_id FROM findings WHERE id = ?", first.ID).Scan(&dv); err != nil {
			t.Fatalf("read dep_version_id: %v", err)
		}
		if dv.Valid != tt.wantValid || dv.Int64 != tt.depVersionID {
			t.Errorf("%s: stored dep_version_id = %v, want %d (valid %v)", tt.name, dv, tt.depVersionID, tt.wantValid)
		}
	}
}

func TestDB_UpsertFinding_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		f    model.Finding
	}{
		{"missing parent repo", model.Finding{Kind: model.KindAdvisory, RepoID: 99, Fingerprint: "fp", LastChangedAt: t0}},
		{"unknown kind", model.Finding{Kind: "rumour", Fingerprint: "fp", LastChangedAt: t0}},
		{"zero last-changed time", model.Finding{Kind: model.KindJobFailure, Fingerprint: "fp"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			if _, err := db.UpsertFinding(t.Context(), tc.f); err == nil {
				t.Fatalf("UpsertFinding(%+v) error = nil, want an error", tc.f)
			}
			if got := count(t, db, "SELECT COUNT(*) FROM findings"); got != 0 {
				t.Errorf("findings rows after failure = %d, want 0", got)
			}
		})
	}
}

func TestDB_UpsertFinding_JoinsCallerTransaction(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	err := db.InTx(t.Context(), func(tx *DB) error {
		if _, err := tx.UpsertFinding(t.Context(), model.Finding{Kind: model.KindJobFailure, Fingerprint: "fp", LastChangedAt: t0}); err != nil {
			return err
		}
		return sql.ErrTxDone // any error: roll back the caller's transaction
	})
	if err == nil {
		t.Fatal("InTx() error = nil, want the returned error")
	}
	if got := count(t, db, "SELECT COUNT(*) FROM findings"); got != 0 {
		t.Errorf("findings rows after rollback = %d, want 0", got)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM finding_state"); got != 0 {
		t.Errorf("finding_state rows after rollback = %d, want 0", got)
	}
}

func TestDB_ResolveFinding(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	f, err := db.UpsertFinding(ctx, model.Finding{Kind: model.KindJobFailure, Fingerprint: "job_failure|vulns", LastChangedAt: t0})
	if err != nil {
		t.Fatalf("UpsertFinding() error = %v", err)
	}

	at := t0.Add(3 * time.Hour)
	if err := db.ResolveFinding(ctx, f.Fingerprint, at); err != nil {
		t.Fatalf("ResolveFinding() error = %v", err)
	}
	readTimes := func() (resolved, changed string) {
		t.Helper()
		if err := db.pool.QueryRowContext(ctx, "SELECT resolved_ts, last_changed_ts FROM findings WHERE id = ?", f.ID).Scan(&resolved, &changed); err != nil {
			t.Fatalf("read times: %v", err)
		}
		return resolved, changed
	}
	resolved, changed := readTimes()
	if resolved != "2026-03-01T15:00:00Z" || changed != "2026-03-01T15:00:00Z" {
		t.Errorf("resolved_ts, last_changed_ts = %s, %s; want both 2026-03-01T15:00:00Z", resolved, changed)
	}

	// A second call does not move the resolved time.
	if err := db.ResolveFinding(ctx, f.Fingerprint, at.Add(time.Hour)); err != nil {
		t.Fatalf("second ResolveFinding() error = %v", err)
	}
	if r2, c2 := readTimes(); r2 != resolved || c2 != changed {
		t.Errorf("second ResolveFinding() changed times to %s, %s", r2, c2)
	}

	if err := db.ResolveFinding(ctx, "no-such-fingerprint", at); err != nil {
		t.Errorf("ResolveFinding(unknown) error = %v, want nil", err)
	}

	open, err := db.ListOpenFindings(ctx)
	if err != nil || open != nil {
		t.Errorf("ListOpenFindings() after resolve = %v, %v; want nil, nil", open, err)
	}
}

func TestDB_ListOpenFindings(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		got, err := db.ListOpenFindings(t.Context())
		if err != nil || got != nil {
			t.Errorf("ListOpenFindings() = %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("order and exclusions", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		ctx := t.Context()
		upsert := func(fp string, score float64) model.Finding {
			t.Helper()
			f, err := db.UpsertFinding(ctx, model.Finding{Kind: model.KindJobFailure, Fingerprint: fp, Score: score, LastChangedAt: t0})
			if err != nil {
				t.Fatalf("UpsertFinding(%s) error = %v", fp, err)
			}
			return f
		}
		low := upsert("low", 1)
		tieA := upsert("tie-a", 5)
		high := upsert("high", 9)
		tieB := upsert("tie-b", 5)
		acked := upsert("acked", 3)
		resolved := upsert("resolved", 8)
		snoozed := upsert("snoozed", 7)
		dismissed := upsert("dismissed", 6)

		exec(t, db, "UPDATE finding_state SET state = 'acked', changed_ts = ?, changed_by = 'operator' WHERE finding_id = ?", ts0, acked.ID)
		exec(t, db, "UPDATE finding_state SET state = 'snoozed', snooze_until_ts = ? WHERE finding_id = ?", "2026-04-01T00:00:00Z", snoozed.ID)
		exec(t, db, "UPDATE finding_state SET state = 'dismissed' WHERE finding_id = ?", dismissed.ID)
		if err := db.ResolveFinding(ctx, resolved.Fingerprint, t0.Add(time.Hour)); err != nil {
			t.Fatalf("ResolveFinding() error = %v", err)
		}

		got, err := db.ListOpenFindings(ctx)
		if err != nil {
			t.Fatalf("ListOpenFindings() error = %v", err)
		}
		acked.State = model.FindingState{State: model.StateAcked, ChangedAt: t0, ChangedBy: "operator"}
		want := []model.Finding{high, tieA, tieB, acked, low}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ListOpenFindings() =\n%+v\nwant\n%+v", got, want)
		}
	})
}
