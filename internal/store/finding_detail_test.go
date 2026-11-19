package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// advPackageID returns the packages row ID of purl.
func advPackageID(t *testing.T, db *DB, purl string) int64 {
	t.Helper()
	var id int64
	if err := db.pool.QueryRowContext(t.Context(), `SELECT id FROM packages WHERE purl = ?`, purl).Scan(&id); err != nil {
		t.Fatalf("look up package %s: %v", purl, err)
	}
	return id
}

// advUpsertFinding calls UpsertFinding and fails the test on error.
func advUpsertFinding(t *testing.T, db *DB, f model.Finding) model.Finding {
	t.Helper()
	got, err := db.UpsertFinding(t.Context(), f)
	if err != nil {
		t.Fatalf("UpsertFinding(%s) error = %v", f.Fingerprint, err)
	}
	return got
}

func TestGetFindingDetail_Advisory(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repoID, ivs := advSeedRepo(t, db, "example-server",
		closeAt(openInterval(pkgLibA, "v1.0.0", 1), 2), openInterval(pkgLibA, "v1.1.0", 2), openInterval(pkgLibB, "v0.1.0", 1))
	// ListIntervals order: lib-a v1.0.0, lib-a v1.1.0, lib-b v0.1.0.
	oldA, newA, libB := ivs[0], ivs[1], ivs[2]
	otherRepo, otherIvs := advSeedRepo(t, db, "example-worker", openInterval(pkgLibA, "v1.0.0", 1))

	adv := advSample("GO-2026-0001")
	adv.Withdrawn = time.Time{}
	advSave(t, db, adv)
	if _, err := db.ApplySignals(ctx, []model.Signal{{Kind: model.SignalKEV, CVE: "CVE-2026-1001", KEVAddedAt: day(3)}}, day(4)); err != nil {
		t.Fatalf("ApplySignals() error = %v", err)
	}

	mNew := model.AdvisoryMatch{RepoID: repoID, DepVersionID: newA.ID, AdvisoryID: adv.ID,
		ExposedFrom: day(2), Confidence: model.ConfidenceHigh, ConfidenceReason: "go.mod pin"}
	mOld := model.AdvisoryMatch{RepoID: repoID, DepVersionID: oldA.ID, AdvisoryID: adv.ID,
		ExposedFrom: day(1), ExposedTo: day(2), FixedVersion: "v1.2.3", Confidence: model.ConfidenceHigh}
	mB := model.AdvisoryMatch{RepoID: repoID, DepVersionID: libB.ID, AdvisoryID: adv.ID,
		ExposedFrom: day(1), Confidence: model.ConfidenceLow}
	// Stored newest first, so row order differs from exposure order.
	if err := db.ReplaceMatches(ctx, repoID, []model.AdvisoryMatch{mNew, mB, mOld}); err != nil {
		t.Fatalf("ReplaceMatches() error = %v", err)
	}
	if err := db.ReplaceMatches(ctx, otherRepo, []model.AdvisoryMatch{{RepoID: otherRepo,
		DepVersionID: otherIvs[0].ID, AdvisoryID: adv.ID, ExposedFrom: day(1), Confidence: model.ConfidenceHigh}}); err != nil {
		t.Fatalf("ReplaceMatches(other repo) error = %v", err)
	}
	stored := advMatches(t, db, repoID)
	ids := map[int64]int64{}
	for _, m := range stored {
		ids[m.DepVersionID] = m.ID
	}
	mOld.ID, mNew.ID = ids[oldA.ID], ids[newA.ID]

	f := advUpsertFinding(t, db, model.Finding{
		Kind:          model.KindAdvisory,
		RepoID:        repoID,
		PackageID:     advPackageID(t, db, pkgLibA.PURL),
		DepVersionID:  newA.ID,
		AdvisoryID:    adv.ID,
		Fingerprint:   "advisory|1|lib-a|GO-2026-0001",
		Score:         8,
		RankReason:    "in KEV",
		LastChangedAt: t0,
	})

	got, err := db.GetFindingDetail(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetFindingDetail() error = %v", err)
	}
	wantAdv := adv
	wantAdv.Raw = nil
	wantAdv.KEV, wantAdv.KEVAddedAt = true, day(3)
	want := model.FindingDetail{
		Finding:  f,
		RepoSlug: "example-server",
		Package:  pkgLibA,
		Interval: newA,
		Advisory: wantAdv,
		Exposures: []model.Exposure{
			{Match: mOld, Interval: oldA},
			{Match: mNew, Interval: newA},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetFindingDetail() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestGetFindingDetail_NoInterval(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID, _ := advSeedRepo(t, db, "example-server", openInterval(pkgLibA, "v1.0.0", 1))
	advSave(t, db, model.Advisory{ID: "GO-2026-0001"})
	f := advUpsertFinding(t, db, model.Finding{
		Kind: model.KindAdvisory, RepoID: repoID, PackageID: advPackageID(t, db, pkgLibA.PURL),
		AdvisoryID: "GO-2026-0001", Fingerprint: "advisory|unlinked", Score: 1, RankReason: "r", LastChangedAt: t0,
	})
	got, err := db.GetFindingDetail(t.Context(), f.ID)
	if err != nil {
		t.Fatalf("GetFindingDetail() error = %v", err)
	}
	want := model.FindingDetail{Finding: f, RepoSlug: "example-server", Package: pkgLibA,
		Advisory: model.Advisory{ID: "GO-2026-0001"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetFindingDetail() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestGetFindingDetail_NotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	_, err := db.GetFindingDetail(t.Context(), 42)
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetFindingDetail(42) error = %v, want model.ErrNotFound", err)
	}
}

func TestGetFindingDetail_JobFailure(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	f := advUpsertFinding(t, db, model.Finding{
		Kind: model.KindJobFailure, Fingerprint: "job_failure|vulns", Score: 5, RankReason: "job failed",
		LastChangedAt: t0,
	})
	got, err := db.GetFindingDetail(t.Context(), f.ID)
	if err != nil {
		t.Fatalf("GetFindingDetail() error = %v", err)
	}
	if want := (model.FindingDetail{Finding: f}); !reflect.DeepEqual(got, want) {
		t.Errorf("GetFindingDetail() = %+v, want %+v", got, want)
	}
}

func TestGetFindingDetail_InsideTransaction(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	f := advUpsertFinding(t, db, model.Finding{
		Kind: model.KindJobFailure, Fingerprint: "job_failure|timeline", Score: 5, RankReason: "job failed",
		LastChangedAt: t0,
	})
	err := db.InTx(t.Context(), func(tx *DB) error {
		got, err := tx.GetFindingDetail(t.Context(), f.ID)
		if err != nil {
			return err
		}
		if got.Finding.ID != f.ID {
			t.Errorf("GetFindingDetail() in transaction = %+v, want finding %d", got, f.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InTx() error = %v", err)
	}
}
