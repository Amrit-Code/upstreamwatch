package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// These tests check the store contracts the M2 jobs rely on across methods:
// the digest's NEW and CHANGED rules depend on what UpsertFinding keeps and
// moves after MarkReported and SaveFindingState.

func m2Get(t *testing.T, db *DB, id int64) model.Finding {
	t.Helper()
	f, err := db.GetFinding(t.Context(), id)
	if err != nil {
		t.Fatalf("GetFinding(%d) error = %v", id, err)
	}
	return f
}

func TestM2_UpsertAfterReportKeepsDigestTimeAndState(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repoID, packageID, advisoryID := seedAdvisoryParents(t, db)
	t1, t2, t3, t4, t5, t6 := day(1), day(2), day(3), day(4), day(5), day(6)
	base := model.Finding{
		Kind: model.KindAdvisory, RepoID: repoID, PackageID: packageID, AdvisoryID: advisoryID,
		Fingerprint: "advisory|example-server|pkg:golang/github.com/example-org/lib-a|GO-2026-0001",
		Score:       100, RankReason: "high 70; direct +10; exposed now +20", LastChangedAt: t1,
	}
	f, err := db.UpsertFinding(ctx, base)
	if err != nil {
		t.Fatalf("UpsertFinding() error = %v", err)
	}
	if err := db.MarkReported(ctx, []int64{f.ID}, t2); err != nil {
		t.Fatalf("MarkReported() error = %v", err)
	}
	acked := model.FindingState{State: model.StateAcked, Note: "planned", ChangedAt: t2, ChangedBy: "operator"}
	if err := db.SaveFindingState(ctx, f.ID, acked); err != nil {
		t.Fatalf("SaveFindingState() error = %v", err)
	}

	steps := []struct {
		name        string
		do          func() error
		wantChanged time.Time
	}{
		{"same score and reason", func() error {
			again := base
			again.LastChangedAt = t3
			_, err := db.UpsertFinding(ctx, again)
			return err
		}, t1},
		{"score up", func() error {
			up := base
			up.Score, up.RankReason, up.LastChangedAt = 130, "high 70; KEV +30; direct +10; exposed now +20", t4
			_, err := db.UpsertFinding(ctx, up)
			return err
		}, t4},
		{"resolved", func() error { return db.ResolveFinding(ctx, base.Fingerprint, t5) }, t5},
		{"back after resolving, same score", func() error {
			back := base
			back.Score, back.RankReason, back.LastChangedAt = 130, "high 70; KEV +30; direct +10; exposed now +20", t6
			_, err := db.UpsertFinding(ctx, back)
			return err
		}, t6},
	}
	for _, st := range steps {
		if err := st.do(); err != nil {
			t.Fatalf("%s: error = %v", st.name, err)
		}
		got := m2Get(t, db, f.ID)
		if !got.LastChangedAt.Equal(st.wantChanged) {
			t.Errorf("%s: LastChangedAt = %v, want %v", st.name, got.LastChangedAt, st.wantChanged)
		}
		if !got.ReportedInDigestAt.Equal(t2) {
			t.Errorf("%s: ReportedInDigestAt = %v, want it kept at %v", st.name, got.ReportedInDigestAt, t2)
		}
		if got.State != acked {
			t.Errorf("%s: State = %+v, want the operator's %+v kept", st.name, got.State, acked)
		}
		if !got.FirstSeenAt.Equal(t1) {
			t.Errorf("%s: FirstSeenAt = %v, want %v", st.name, got.FirstSeenAt, t1)
		}
	}
	if got := m2Get(t, db, f.ID); !got.ResolvedAt.IsZero() {
		t.Errorf("ResolvedAt = %v after the finding came back, want zero", got.ResolvedAt)
	}
}

func TestM2_ListFindings_EqualScoresByID(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	var ids []int64
	for _, s := range []struct {
		job   string
		score float64
	}{{"b", 50}, {"a", 50}, {"c", 80}, {"d", 50}} {
		f, err := db.UpsertFinding(ctx, model.Finding{
			Kind: model.KindJobFailure, Fingerprint: "job_failure|" + s.job, Score: s.score, LastChangedAt: t0,
		})
		if err != nil {
			t.Fatalf("UpsertFinding(%s) error = %v", s.job, err)
		}
		ids = append(ids, f.ID)
	}
	got, err := db.ListFindings(ctx, model.FindingFilter{Now: t0})
	if err != nil {
		t.Fatalf("ListFindings() error = %v", err)
	}
	if want := []int64{ids[2], ids[0], ids[1], ids[3]}; !reflect.DeepEqual(findingIDs(got), want) {
		t.Errorf("ListFindings() IDs = %v, want %v (score, then lowest ID)", findingIDs(got), want)
	}
}

func TestM2_ReplaceMatches_EmptyClearsOnlyThatRepo(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	serverID, serverIvs := advSeedRepo(t, db, "example-server", openInterval(pkgLibA, "v1.0.0", 1))
	workerID, workerIvs := advSeedRepo(t, db, "example-worker", openInterval(pkgLibA, "v1.0.0", 1))
	advSave(t, db, model.Advisory{ID: "GO-2026-0001", Modified: day(1)})
	match := func(repoID, depID int64) model.AdvisoryMatch {
		return model.AdvisoryMatch{RepoID: repoID, DepVersionID: depID, AdvisoryID: "GO-2026-0001",
			ExposedFrom: day(1), Confidence: model.ConfidenceHigh}
	}
	if err := db.ReplaceMatches(ctx, serverID, []model.AdvisoryMatch{match(serverID, serverIvs[0].ID)}); err != nil {
		t.Fatalf("ReplaceMatches(server) error = %v", err)
	}
	if err := db.ReplaceMatches(ctx, workerID, []model.AdvisoryMatch{match(workerID, workerIvs[0].ID)}); err != nil {
		t.Fatalf("ReplaceMatches(worker) error = %v", err)
	}

	if err := db.ReplaceMatches(ctx, serverID, nil); err != nil {
		t.Fatalf("ReplaceMatches(server, nil) error = %v", err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM advisory_matches WHERE repo_id = ?`, serverID); n != 0 {
		t.Errorf("server matches after an empty replace = %d, want 0", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM advisory_matches WHERE repo_id = ?`, workerID); n != 1 {
		t.Errorf("worker matches = %d, want its 1 untouched", n)
	}
}
