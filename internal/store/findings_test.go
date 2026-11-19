package store

import (
	"database/sql"
	"errors"
	"fmt"
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
	// Two intervals need two opening commits: an interval is unique by repo,
	// manifest, package and opening commit.
	for _, c := range []struct{ id, from int64 }{{5, 4}, {8, 6}} {
		exec(t, db, `INSERT INTO commits (id, repo_id, sha, created_ts, updated_ts) VALUES (?, ?, ?, ?, ?)`,
			c.from, repoID, fmt.Sprintf("abc%d", c.from), ts0, ts0)
		exec(t, db, `INSERT INTO dep_versions (id, repo_id, package_id, manifest_id, version, from_commit_id, created_ts, updated_ts)
			VALUES (?, ?, ?, 3, 'v1.2.0', ?, ?, ?)`, c.id, repoID, packageID, c.from, ts0, ts0)
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

// lastChanged reads a finding's stored last_changed_ts and resolved_ts.
func lastChanged(t *testing.T, db *DB, id int64) (changed string, resolved sql.NullString) {
	t.Helper()
	if err := db.pool.QueryRowContext(t.Context(),
		"SELECT last_changed_ts, resolved_ts FROM findings WHERE id = ?", id).Scan(&changed, &resolved); err != nil {
		t.Fatalf("read finding %d times: %v", id, err)
	}
	return changed, resolved
}

func TestUpsertFinding_LastChangedOnlyOnChange(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	const fp = "job_failure|vulns"
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	first, err := db.UpsertFinding(ctx, model.Finding{
		Kind: model.KindJobFailure, Fingerprint: fp, Score: 2, RankReason: "job failed", LastChangedAt: at(1),
	})
	if err != nil {
		t.Fatalf("first UpsertFinding() error = %v", err)
	}

	// Each step runs on the state the previous one left, so they run in order.
	steps := []struct {
		name        string
		resolveAt   time.Time // when set, ResolveFinding runs first
		score       float64
		reason      string
		at          time.Time
		wantChanged time.Time
	}{
		{name: "same score and reason keeps the time", score: 2, reason: "job failed", at: at(2), wantChanged: at(1)},
		{name: "new score moves it", score: 3, reason: "job failed", at: at(3), wantChanged: at(3)},
		{name: "new rank reason moves it", score: 3, reason: "job failed twice", at: at(4), wantChanged: at(4)},
		{name: "unchanged again keeps it", score: 3, reason: "job failed twice", at: at(5), wantChanged: at(4)},
		{name: "reopening a resolved finding moves it", resolveAt: at(6), score: 3, reason: "job failed twice", at: at(7), wantChanged: at(7)},
	}
	for _, st := range steps {
		if !st.resolveAt.IsZero() {
			if err := db.ResolveFinding(ctx, fp, st.resolveAt); err != nil {
				t.Fatalf("%s: ResolveFinding() error = %v", st.name, err)
			}
		}
		got, err := db.UpsertFinding(ctx, model.Finding{
			Kind: model.KindJobFailure, Fingerprint: fp, Score: st.score, RankReason: st.reason, LastChangedAt: st.at,
		})
		if err != nil {
			t.Fatalf("%s: UpsertFinding() error = %v", st.name, err)
		}
		if got.ID != first.ID || !got.LastChangedAt.Equal(st.wantChanged) || !got.ResolvedAt.IsZero() {
			t.Errorf("%s: UpsertFinding() = ID %d last changed %v resolved %v; want ID %d last changed %v, not resolved",
				st.name, got.ID, got.LastChangedAt, got.ResolvedAt, first.ID, st.wantChanged)
		}
		if got.Score != st.score || got.RankReason != st.reason || !got.FirstSeenAt.Equal(at(1)) {
			t.Errorf("%s: UpsertFinding() = score %v reason %q first seen %v; want %v %q %v",
				st.name, got.Score, got.RankReason, got.FirstSeenAt, st.score, st.reason, at(1))
		}
		changed, resolved := lastChanged(t, db, first.ID)
		if want := formatTime(st.wantChanged); changed != want || resolved.Valid {
			t.Errorf("%s: stored last_changed_ts, resolved_ts = %s, %v; want %s, NULL", st.name, changed, resolved, want)
		}
	}
}

func TestListUnresolvedFindings(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repoID, packageID, advisoryID := seedAdvisoryParents(t, db)

	if got, err := db.ListUnresolvedFindings(ctx, model.KindAdvisory); err != nil || got != nil {
		t.Errorf("ListUnresolvedFindings() on an empty table = %v, %v; want nil, nil", got, err)
	}
	adv := func(fp string, score float64) model.Finding {
		t.Helper()
		f, err := db.UpsertFinding(ctx, model.Finding{Kind: model.KindAdvisory, RepoID: repoID, PackageID: packageID,
			AdvisoryID: advisoryID, Fingerprint: fp, Score: score, LastChangedAt: t0})
		if err != nil {
			t.Fatalf("UpsertFinding(%s) error = %v", fp, err)
		}
		return f
	}
	a1 := adv("a1", 1)
	a2 := adv("a2", 9) // higher score, but the order is by ID
	resolved := adv("resolved", 5)
	dismissed := adv("dismissed", 3)
	if _, err := db.UpsertFinding(ctx, model.Finding{Kind: model.KindJobFailure, Fingerprint: "job_failure|vulns", LastChangedAt: t0}); err != nil {
		t.Fatalf("UpsertFinding(job failure) error = %v", err)
	}
	if err := db.ResolveFinding(ctx, resolved.Fingerprint, t0); err != nil {
		t.Fatalf("ResolveFinding() error = %v", err)
	}
	dismissed.State = model.FindingState{State: model.StateDismissed, ChangedAt: t0, ChangedBy: "operator"}
	if err := db.SaveFindingState(ctx, dismissed.ID, dismissed.State); err != nil {
		t.Fatalf("SaveFindingState() error = %v", err)
	}

	got, err := db.ListUnresolvedFindings(ctx, model.KindAdvisory)
	if err != nil {
		t.Fatalf("ListUnresolvedFindings() error = %v", err)
	}
	want := []model.Finding{a1, a2, dismissed}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListUnresolvedFindings(advisory) =\n%+v\nwant\n%+v", got, want)
	}
	if got, err := db.ListUnresolvedFindings(ctx, model.KindIssue); err != nil || got != nil {
		t.Errorf("ListUnresolvedFindings(issue) = %v, %v; want nil, nil", got, err)
	}
}

// findingIDs returns the IDs of fs in order.
func findingIDs(fs []model.Finding) []int64 {
	var ids []int64
	for _, f := range fs {
		ids = append(ids, f.ID)
	}
	return ids
}

func TestListFindings_EffectiveState(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repoID, packageID, advisoryID := seedAdvisoryParents(t, db)

	upsert := func(kind model.FindingKind, fp string, score float64) model.Finding {
		t.Helper()
		f := model.Finding{Kind: kind, Fingerprint: fp, Score: score, LastChangedAt: t0}
		if kind == model.KindAdvisory {
			f.RepoID, f.PackageID, f.AdvisoryID = repoID, packageID, advisoryID
		}
		got, err := db.UpsertFinding(ctx, f)
		if err != nil {
			t.Fatalf("UpsertFinding(%s) error = %v", fp, err)
		}
		return got
	}
	setState := func(f model.Finding, st model.FindingState) {
		t.Helper()
		if err := db.SaveFindingState(ctx, f.ID, st); err != nil {
			t.Fatalf("SaveFindingState(%d) error = %v", f.ID, err)
		}
	}
	open := upsert(model.KindAdvisory, "open", 5)
	acked := upsert(model.KindJobFailure, "job_failure|acked", 4)
	snoozedFuture := upsert(model.KindAdvisory, "snoozed-future", 7)
	snoozedPast := upsert(model.KindJobFailure, "job_failure|snoozed-past", 6)
	dismissed := upsert(model.KindAdvisory, "dismissed", 8)
	resolved := upsert(model.KindAdvisory, "resolved", 9)
	setState(acked, model.FindingState{State: model.StateAcked, ChangedAt: t0, ChangedBy: "operator"})
	setState(snoozedFuture, model.FindingState{State: model.StateSnoozed, SnoozeUntil: t0.Add(24 * time.Hour), ChangedAt: t0})
	setState(snoozedPast, model.FindingState{State: model.StateSnoozed, SnoozeUntil: t0.Add(-time.Hour), ChangedAt: t0.Add(-48 * time.Hour)})
	setState(dismissed, model.FindingState{State: model.StateDismissed, Note: "not used", ChangedAt: t0})
	if err := db.ResolveFinding(ctx, resolved.Fingerprint, t0); err != nil {
		t.Fatalf("ResolveFinding() error = %v", err)
	}

	tests := []struct {
		name string
		flt  model.FindingFilter
		want []int64
	}{
		{"open, acked and expired snooze by score", model.FindingFilter{Now: t0},
			[]int64{snoozedPast.ID, open.ID, acked.ID}},
		{"suppressed shows every unresolved finding", model.FindingFilter{Now: t0, Suppressed: true},
			[]int64{dismissed.ID, snoozedFuture.ID, snoozedPast.ID, open.ID, acked.ID}},
		{"repo filter", model.FindingFilter{Now: t0, RepoID: repoID},
			[]int64{open.ID}},
		{"repo filter with suppressed", model.FindingFilter{Now: t0, RepoID: repoID, Suppressed: true},
			[]int64{dismissed.ID, snoozedFuture.ID, open.ID}},
		{"kind filter", model.FindingFilter{Now: t0, Kind: model.KindJobFailure},
			[]int64{snoozedPast.ID, acked.ID}},
		{"unknown repo", model.FindingFilter{Now: t0, RepoID: 99, Suppressed: true}, nil},
		{"snooze ending exactly now counts as open", model.FindingFilter{Now: t0.Add(24 * time.Hour)},
			[]int64{snoozedFuture.ID, snoozedPast.ID, open.ID, acked.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := db.ListFindings(ctx, tt.flt)
			if err != nil {
				t.Fatalf("ListFindings(%+v) error = %v", tt.flt, err)
			}
			if ids := findingIDs(got); !reflect.DeepEqual(ids, tt.want) {
				t.Errorf("ListFindings(%+v) IDs = %v, want %v", tt.flt, ids, tt.want)
			}
		})
	}

	t.Run("rows carry the stored state", func(t *testing.T) {
		got, err := db.ListFindings(ctx, model.FindingFilter{Now: t0})
		if err != nil || len(got) == 0 {
			t.Fatalf("ListFindings() = %v, %v; want rows", got, err)
		}
		want, err := db.GetFinding(ctx, snoozedPast.ID)
		if err != nil {
			t.Fatalf("GetFinding() error = %v", err)
		}
		if want.State.State != model.StateSnoozed || !reflect.DeepEqual(got[0], want) {
			t.Errorf("ListFindings()[0] = %+v, want the stored snoozed finding %+v", got[0], want)
		}
	})

	t.Run("zero now is an error", func(t *testing.T) {
		if got, err := db.ListFindings(ctx, model.FindingFilter{Suppressed: true}); err == nil {
			t.Errorf("ListFindings(zero Now) = %v, nil; want an error", got)
		}
	})
}

func TestGetFinding_NotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	_, err := db.GetFinding(t.Context(), 42)
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetFinding(42) error = %v, want one wrapping model.ErrNotFound", err)
	}
}

func TestGetFinding_Found(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	stored, err := db.UpsertFinding(t.Context(), model.Finding{
		Kind: model.KindJobFailure, Fingerprint: "job_failure|ingest", Score: 1, RankReason: "job failed", LastChangedAt: t0,
	})
	if err != nil {
		t.Fatalf("UpsertFinding() error = %v", err)
	}
	got, err := db.GetFinding(t.Context(), stored.ID)
	if err != nil || !reflect.DeepEqual(got, stored) {
		t.Errorf("GetFinding(%d) = %+v, %v; want %+v, nil", stored.ID, got, err, stored)
	}
}

func TestSaveFindingState_RoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	f, err := db.UpsertFinding(ctx, model.Finding{Kind: model.KindJobFailure, Fingerprint: "job_failure|vulns", LastChangedAt: t0})
	if err != nil {
		t.Fatalf("UpsertFinding() error = %v", err)
	}

	tests := []struct {
		name string
		st   model.FindingState
	}{
		{"snoozed with every field", model.FindingState{
			State:       model.StateSnoozed,
			SnoozeUntil: t0.Add(7 * 24 * time.Hour),
			Note:        "waiting for the next release",
			ChangedAt:   t0.Add(time.Hour),
			ChangedBy:   "operator",
		}},
		{"open with empty fields", model.FindingState{State: model.StateOpen}},
	}
	for i, tt := range tests {
		// Each save happens at a later store time, so updated_ts must follow it.
		clock := t0.Add(time.Duration(i+1) * time.Hour)
		db.now = func() time.Time { return clock }
		if err := db.SaveFindingState(ctx, f.ID, tt.st); err != nil {
			t.Fatalf("%s: SaveFindingState() error = %v", tt.name, err)
		}
		got, err := db.GetFinding(ctx, f.ID)
		if err != nil {
			t.Fatalf("%s: GetFinding() error = %v", tt.name, err)
		}
		if !reflect.DeepEqual(got.State, tt.st) {
			t.Errorf("%s: stored state = %+v, want %+v", tt.name, got.State, tt.st)
		}
		if !got.LastChangedAt.Equal(t0) {
			t.Errorf("%s: last changed = %v, want %v (state changes do not touch the finding)", tt.name, got.LastChangedAt, t0)
		}
		var snooze, note, changed, by sql.NullString
		var created, updated string
		if err := db.pool.QueryRowContext(ctx,
			`SELECT snooze_until_ts, note, changed_ts, changed_by, created_ts, updated_ts FROM finding_state WHERE finding_id = ?`, f.ID).
			Scan(&snooze, &note, &changed, &by, &created, &updated); err != nil {
			t.Fatalf("%s: read finding_state: %v", tt.name, err)
		}
		wantNull := tt.st.SnoozeUntil.IsZero()
		if snooze.Valid == wantNull || note.Valid == wantNull || changed.Valid == wantNull || by.Valid == wantNull {
			t.Errorf("%s: stored optional columns = %v %v %v %v, want all NULL = %v", tt.name, snooze, note, changed, by, wantNull)
		}
		if created != ts0 || updated != formatTime(clock) {
			t.Errorf("%s: created_ts, updated_ts = %s, %s; want %s, %s", tt.name, created, updated, ts0, formatTime(clock))
		}
	}

	t.Run("unknown ID", func(t *testing.T) {
		err := db.SaveFindingState(ctx, f.ID+100, model.FindingState{State: model.StateAcked})
		if !errors.Is(err, model.ErrNotFound) {
			t.Errorf("SaveFindingState(unknown) error = %v, want one wrapping model.ErrNotFound", err)
		}
	})

	t.Run("unknown state value is refused", func(t *testing.T) {
		if err := db.SaveFindingState(ctx, f.ID, model.FindingState{State: "hidden"}); err == nil {
			t.Errorf("SaveFindingState(state hidden) error = nil, want the CHECK constraint to refuse it")
		}
	})
}

func TestMarkReported(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	var fs []model.Finding
	for _, fp := range []string{"job_failure|a", "job_failure|b", "job_failure|c"} {
		f, err := db.UpsertFinding(ctx, model.Finding{Kind: model.KindJobFailure, Fingerprint: fp, LastChangedAt: t0})
		if err != nil {
			t.Fatalf("UpsertFinding(%s) error = %v", fp, err)
		}
		fs = append(fs, f)
	}
	reported := func(id int64) (reported sql.NullString, changed, updated string) {
		t.Helper()
		if err := db.pool.QueryRowContext(ctx,
			"SELECT reported_in_digest_ts, last_changed_ts, updated_ts FROM findings WHERE id = ?", id).
			Scan(&reported, &changed, &updated); err != nil {
			t.Fatalf("read finding %d: %v", id, err)
		}
		return reported, changed, updated
	}

	t.Run("empty is a no-op", func(t *testing.T) {
		if err := db.MarkReported(ctx, nil, t0); err != nil {
			t.Fatalf("MarkReported(nil) error = %v", err)
		}
		if n := count(t, db, "SELECT COUNT(*) FROM findings WHERE reported_in_digest_ts IS NOT NULL"); n != 0 {
			t.Errorf("reported findings after an empty MarkReported = %d, want 0", n)
		}
	})

	t.Run("marks only the given rows across chunks", func(t *testing.T) {
		// 600 IDs force two statements; the real IDs sit in both chunks, the
		// rest name no finding.
		ids := make([]int64, 0, 600)
		for i := range 600 {
			ids = append(ids, int64(1000+i))
		}
		ids[0], ids[550] = fs[0].ID, fs[2].ID
		db.now = func() time.Time { return t0.Add(time.Hour) }
		at := t0.Add(30 * time.Minute)
		if err := db.MarkReported(ctx, ids, at); err != nil {
			t.Fatalf("MarkReported() error = %v", err)
		}
		for _, f := range []model.Finding{fs[0], fs[2]} {
			rep, changed, updated := reported(f.ID)
			if rep.String != formatTime(at) || changed != ts0 || updated != "2026-03-01T13:00:00Z" {
				t.Errorf("finding %d reported, last changed, updated = %v, %s, %s; want %s, %s, 2026-03-01T13:00:00Z",
					f.ID, rep, changed, updated, formatTime(at), ts0)
			}
		}
		if rep, _, updated := reported(fs[1].ID); rep.Valid || updated != ts0 {
			t.Errorf("unlisted finding reported, updated = %v, %s; want NULL, %s", rep, updated, ts0)
		}
	})
}
