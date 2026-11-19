package findings_test

import (
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/findings"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

// These tests run findings.Service over a real SQLite file, opened and
// migrated as start-up does it, and read the result back the way the digest
// and the findings page do.

var (
	_ findings.Store = (*store.DB)(nil)

	base = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
)

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

func raise(t *testing.T, db *store.DB, job string) model.Finding {
	t.Helper()
	f, err := db.UpsertFinding(t.Context(), model.Finding{
		Kind: model.KindJobFailure, Fingerprint: "job_failure|" + job, Score: model.JobFailureScore,
		RankReason: "job " + job + " failed", LastChangedAt: base.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("UpsertFinding(%s) error = %v", job, err)
	}
	return f
}

// listed returns the IDs ListFindings returns at now.
func listed(t *testing.T, db *store.DB, now time.Time, suppressed bool) []int64 {
	t.Helper()
	fs, err := db.ListFindings(t.Context(), model.FindingFilter{Now: now, Suppressed: suppressed})
	if err != nil {
		t.Fatalf("ListFindings() error = %v", err)
	}
	var ids []int64
	for _, f := range fs {
		ids = append(ids, f.ID)
	}
	return ids
}

func TestService_RealStore(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	ctx := t.Context()
	clock := base
	svc := findings.NewService(db, func() time.Time { return clock })
	f := raise(t, db, "vulns")

	// Ack: the stored state is what Ack returned, and an acked finding is
	// still listed.
	acked, err := svc.Ack(ctx, f.ID, "  upgrade planned\n", "operator")
	if err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	want := model.FindingState{State: model.StateAcked, Note: "upgrade planned", ChangedAt: base, ChangedBy: "operator"}
	if acked != want {
		t.Errorf("Ack() = %+v, want %+v", acked, want)
	}
	got, err := db.GetFinding(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetFinding() error = %v", err)
	}
	if got.State != want {
		t.Errorf("stored state after Ack = %+v, want %+v", got.State, want)
	}
	if ids := listed(t, db, clock, false); !slices.Equal(ids, []int64{f.ID}) {
		t.Errorf("ListFindings after Ack = %v, want [%d]", ids, f.ID)
	}

	// Snooze for a day: hidden until the day is over, then open again.
	clock = base.Add(time.Hour)
	snoozed, err := svc.Snooze(ctx, f.ID, 24*time.Hour, "", "operator")
	if err != nil {
		t.Fatalf("Snooze() error = %v", err)
	}
	if !snoozed.SnoozeUntil.Equal(clock.Add(24 * time.Hour)) {
		t.Errorf("SnoozeUntil = %v, want %v", snoozed.SnoozeUntil, clock.Add(24*time.Hour))
	}
	for _, step := range []struct {
		at         time.Time
		wantListed bool
	}{
		{clock, false},
		{clock.Add(24*time.Hour - time.Second), false},
		{clock.Add(24 * time.Hour), true},
	} {
		if ids := listed(t, db, step.at, false); slices.Contains(ids, f.ID) != step.wantListed {
			t.Errorf("ListFindings at %v = %v, want listed %v", step.at, ids, step.wantListed)
		}
		if ids := listed(t, db, step.at, true); !slices.Contains(ids, f.ID) {
			t.Errorf("ListFindings(suppressed) at %v = %v, want it listed", step.at, ids)
		}
	}

	// Dismiss: hidden unless suppressed findings are asked for.
	clock = base.Add(2 * time.Hour)
	if _, err := svc.Dismiss(ctx, f.ID, "not reachable", "operator"); err != nil {
		t.Fatalf("Dismiss() error = %v", err)
	}
	if ids := listed(t, db, clock.Add(48*time.Hour), false); slices.Contains(ids, f.ID) {
		t.Errorf("ListFindings after Dismiss = %v, want %d hidden", ids, f.ID)
	}

	// Reopen: open again, with the note cleared.
	clock = base.Add(3 * time.Hour)
	if _, err := svc.Reopen(ctx, f.ID, "operator"); err != nil {
		t.Fatalf("Reopen() error = %v", err)
	}
	got, err = db.GetFinding(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetFinding() error = %v", err)
	}
	want = model.FindingState{State: model.StateOpen, ChangedAt: clock, ChangedBy: "operator"}
	if got.State != want {
		t.Errorf("stored state after Reopen = %+v, want %+v", got.State, want)
	}
}

func TestService_RealStoreErrors(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	ctx := t.Context()
	svc := findings.NewService(db, func() time.Time { return base })

	if _, err := svc.Ack(ctx, 404, "", "operator"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("Ack(404) error = %v, want model.ErrNotFound", err)
	}

	f := raise(t, db, "ingest")
	if err := db.ResolveFinding(ctx, f.Fingerprint, base.Add(-time.Minute)); err != nil {
		t.Fatalf("ResolveFinding() error = %v", err)
	}
	before, err := db.GetFinding(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetFinding() error = %v", err)
	}
	for name, act := range map[string]func() error{
		"ack":     func() error { _, err := svc.Ack(ctx, f.ID, "", "operator"); return err },
		"snooze":  func() error { _, err := svc.Snooze(ctx, f.ID, 7*24*time.Hour, "", "operator"); return err },
		"dismiss": func() error { _, err := svc.Dismiss(ctx, f.ID, "", "operator"); return err },
		"reopen":  func() error { _, err := svc.Reopen(ctx, f.ID, "operator"); return err },
	} {
		if err := act(); !errors.Is(err, model.ErrInvalidTransition) {
			t.Errorf("%s on a resolved finding: error = %v, want model.ErrInvalidTransition", name, err)
		}
	}
	after, err := db.GetFinding(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetFinding() error = %v", err)
	}
	if after.State != before.State {
		t.Errorf("state of a resolved finding changed from %+v to %+v", before.State, after.State)
	}
}
