package report_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/report"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

// These tests run report.Service over a real SQLite file, opened and migrated
// as start-up does it, with recording notifiers.

var _ report.Store = (*store.DB)(nil)

var t0 = time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

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

// recorder is a report.Notifier that keeps every digest it is sent.
type recorder struct{ sent []model.Digest }

func (r *recorder) Send(_ context.Context, d model.Digest) error {
	r.sent = append(r.sent, d)
	return nil
}

// raiseJobFailure stores an open job_failure finding for job.
func raiseJobFailure(t *testing.T, db *store.DB, job string, at time.Time) model.Finding {
	t.Helper()
	f, err := db.UpsertFinding(t.Context(), model.Finding{
		Kind:          model.KindJobFailure,
		Fingerprint:   "job_failure|" + job,
		Score:         model.JobFailureScore,
		RankReason:    "job " + job + " failed: status 503",
		LastChangedAt: at,
	})
	if err != nil {
		t.Fatalf("UpsertFinding(%s) error = %v", job, err)
	}
	return f
}

func newService(db *store.DB, n report.Notifier, now *time.Time) *report.Service {
	return report.NewService(db, []report.Channel{{Name: "email", Notifier: n}}, report.Options{
		BaseURL: "https://upw.example.test",
		Now:     func() time.Time { return *now },
	}, slog.New(slog.DiscardHandler))
}

func TestRun_NothingThenSkip(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	f := raiseJobFailure(t, db, "vulns", t0.Add(-time.Hour))
	n := &recorder{}
	now := t0
	s := newService(db, n, &now)

	stats, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if stats["new"] != 1 || stats["sent"] != 1 {
		t.Errorf("first run stats = %v, want new 1 and sent 1", stats)
	}
	first, err := db.LastSentDigest(t.Context())
	if err != nil {
		t.Fatalf("LastSentDigest() error = %v", err)
	}
	got, err := db.GetFinding(t.Context(), f.ID)
	if err != nil {
		t.Fatalf("GetFinding() error = %v", err)
	}
	if !got.ReportedInDigestAt.Equal(t0) {
		t.Errorf("ReportedInDigestAt = %v, want %v", got.ReportedInDigestAt, t0)
	}

	now = t0.Add(24 * time.Hour)
	stats, err = s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if stats["skipped"] != 1 || stats["sent"] != 0 {
		t.Errorf("second run stats = %v, want skipped 1 and sent 0", stats)
	}
	if len(n.sent) != 1 {
		t.Errorf("notifier got %d digests, want 1", len(n.sent))
	}
	// A second row would be the latest sent row or a failed one.
	last, err := db.LastSentDigest(t.Context())
	if err != nil {
		t.Fatalf("LastSentDigest() error = %v", err)
	}
	if last.ID != first.ID {
		t.Errorf("last sent digest ID = %d, want %d (the first run's row)", last.ID, first.ID)
	}
	failed, err := db.FailedDigests(t.Context())
	if err != nil {
		t.Fatalf("FailedDigests() error = %v", err)
	}
	if len(failed) != 0 {
		t.Errorf("failed digests = %+v, want none", failed)
	}
}

func TestRun_AckedLeftOut(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	acked := raiseJobFailure(t, db, "ingest", t0.Add(-2*time.Hour))
	fresh := raiseJobFailure(t, db, "vulns", t0.Add(-time.Hour))
	if err := db.SaveFindingState(t.Context(), acked.ID, model.FindingState{
		State: model.StateAcked, ChangedAt: t0.Add(-90 * time.Minute), ChangedBy: "operator",
	}); err != nil {
		t.Fatalf("SaveFindingState() error = %v", err)
	}
	n := &recorder{}
	now := t0
	s := newService(db, n, &now)

	if _, err := s.Run(t.Context(), model.Scope{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(n.sent) != 1 {
		t.Fatalf("notifier got %d digests, want 1", len(n.sent))
	}
	d := n.sent[0]
	var ids []int64
	for _, it := range d.New {
		ids = append(ids, it.FindingID)
	}
	if want := []int64{fresh.ID}; !reflect.DeepEqual(ids, want) || len(d.Changed) != 0 {
		t.Errorf("New IDs = %v, Changed = %d, want %v and 0", ids, len(d.Changed), want)
	}
	if d.Open != 1 || d.Acked != 1 {
		t.Errorf("open/acked = %d/%d, want 1/1", d.Open, d.Acked)
	}
	if it := d.New[0]; it.Tag != "[J]" || it.Headline != "job vulns failed" {
		t.Errorf("item tag/headline = %q %q, want [J] and %q", it.Tag, it.Headline, "job vulns failed")
	}
	got, err := db.GetFinding(t.Context(), acked.ID)
	if err != nil {
		t.Fatalf("GetFinding() error = %v", err)
	}
	if !got.ReportedInDigestAt.IsZero() {
		t.Errorf("acked finding ReportedInDigestAt = %v, want unset", got.ReportedInDigestAt)
	}
	if _, err := db.LastSentDigest(t.Context()); err != nil {
		t.Errorf("LastSentDigest() error = %v, want the run's sent row", err)
	}
}
