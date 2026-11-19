package query_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

// These tests read through query.Service over a real SQLite file that the real
// pipeline.Runner has written to. They run inside a synctest bubble so the
// Runner's clock is fake: times are whole seconds and can be compared exactly.

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

type funcJob struct {
	name string
	fn   func() (model.Stats, error)
}

func (j funcJob) Name() string { return j.name }

func (j funcJob) Run(context.Context, model.Scope) (model.Stats, error) { return j.fn() }

func TestIntegration_ServiceReadsWhatRunnerWrote(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		ctx := t.Context()
		jobErr := errors.New("fetch example-org/example-server: 502 bad gateway")
		// vulns succeeds and runs first; ingest fails and runs second. The
		// documented order is by job name, so ingest comes first.
		r, err := pipeline.NewRunner([]pipeline.Job{
			funcJob{name: "vulns", fn: func() (model.Stats, error) { return model.Stats{"matched": 2}, nil }},
			funcJob{name: "ingest", fn: func() (model.Stats, error) { return nil, jobErr }},
		}, db, db, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("NewRunner() error = %v", err)
		}

		start := time.Now().UTC()
		vulnsRun, err := r.Run(ctx, "vulns", model.Scope{})
		if err != nil {
			t.Fatalf("Run(vulns) error = %v", err)
		}
		failedAt := time.Now().UTC()
		ingestRun, err := r.Run(ctx, "ingest", model.Scope{})
		if !errors.Is(err, jobErr) {
			t.Fatalf("Run(ingest) error = %v, want %v", err, jobErr)
		}

		svc := query.NewService(db, db, db, slog.New(slog.DiscardHandler))

		gotRuns, err := svc.LastRuns(ctx)
		if err != nil {
			t.Fatalf("LastRuns() error = %v", err)
		}
		wantRuns := []model.Run{
			{ID: ingestRun.ID, Job: "ingest", StartedAt: failedAt, FinishedAt: failedAt, Status: model.RunFailed, Error: jobErr.Error()},
			{ID: vulnsRun.ID, Job: "vulns", StartedAt: start, FinishedAt: start, Status: model.RunSucceeded, Stats: model.Stats{"matched": 2}},
		}
		if !reflect.DeepEqual(gotRuns, wantRuns) {
			t.Errorf("LastRuns() =\n%+v\nwant\n%+v", gotRuns, wantRuns)
		}
		storeRuns, err := db.LatestRuns(ctx)
		if err != nil {
			t.Fatalf("db.LatestRuns() error = %v", err)
		}
		if !reflect.DeepEqual(gotRuns, storeRuns) {
			t.Errorf("LastRuns() = %+v, want what the store holds %+v", gotRuns, storeRuns)
		}

		gotFindings, err := svc.OpenFindings(ctx)
		if err != nil {
			t.Fatalf("OpenFindings() error = %v", err)
		}
		if len(gotFindings) != 1 {
			t.Fatalf("OpenFindings() = %+v, want one finding", gotFindings)
		}
		wantFinding := model.Finding{
			ID:            gotFindings[0].ID,
			Kind:          model.KindJobFailure,
			Fingerprint:   "job_failure|ingest",
			Score:         50,
			RankReason:    "job ingest failed: " + jobErr.Error(),
			FirstSeenAt:   failedAt,
			LastChangedAt: failedAt,
			State:         model.FindingState{State: model.StateOpen},
		}
		if gotFindings[0].ID == 0 || !reflect.DeepEqual(gotFindings[0], wantFinding) {
			t.Errorf("OpenFindings()[0] = %+v, want %+v", gotFindings[0], wantFinding)
		}
		storeFindings, err := db.ListOpenFindings(ctx)
		if err != nil {
			t.Fatalf("db.ListOpenFindings() error = %v", err)
		}
		if !reflect.DeepEqual(gotFindings, storeFindings) {
			t.Errorf("OpenFindings() = %+v, want what the store holds %+v", gotFindings, storeFindings)
		}
	})
}

func TestIntegration_ServiceOrdersFindingsAndDropsResolved(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		ctx := t.Context()
		fail := func() (model.Stats, error) { return nil, errors.New("down") }
		recovered := false
		r, err := pipeline.NewRunner([]pipeline.Job{
			funcJob{name: "zeta", fn: fail},
			funcJob{name: "alpha", fn: fail},
			funcJob{name: "mid", fn: func() (model.Stats, error) {
				if recovered {
					return nil, nil
				}
				return nil, errors.New("down")
			}},
		}, db, db, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("NewRunner() error = %v", err)
		}
		// Every job_failure scores 50, so the tie is broken by lowest ID:
		// the order in which the findings were first raised.
		for _, name := range []string{"zeta", "mid", "alpha"} {
			_, _ = r.Run(ctx, name, model.Scope{}) // each fails on purpose
		}
		recovered = true
		if _, err := r.Run(ctx, "mid", model.Scope{}); err != nil {
			t.Fatalf("Run(mid) after recovery error = %v", err)
		}

		got, err := query.NewService(db, db, db, slog.New(slog.DiscardHandler)).OpenFindings(ctx)
		if err != nil {
			t.Fatalf("OpenFindings() error = %v", err)
		}
		var fps []string
		for _, f := range got {
			fps = append(fps, f.Fingerprint)
		}
		want := []string{"job_failure|zeta", "job_failure|alpha"}
		if !reflect.DeepEqual(fps, want) {
			t.Errorf("OpenFindings() fingerprints = %q, want %q", fps, want)
		}
	})
}

func TestIntegration_ServiceOnEmptyStore(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	svc := query.NewService(db, db, db, slog.New(slog.DiscardHandler))
	runs, err := svc.LastRuns(t.Context())
	if err != nil || runs != nil {
		t.Errorf("LastRuns() on an empty store = %+v, %v; want nil, nil", runs, err)
	}
	findings, err := svc.OpenFindings(t.Context())
	if err != nil || findings != nil {
		t.Errorf("OpenFindings() on an empty store = %+v, %v; want nil, nil", findings, err)
	}
}

func TestIntegration_ServiceOnClosedStore(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "upw.sqlite")
	db, err := store.Open(t.Context(), path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	svc := query.NewService(db, db, db, slog.New(slog.DiscardHandler))
	if _, err := svc.LastRuns(t.Context()); err == nil {
		t.Errorf("LastRuns() on a closed store error = nil, want an error")
	}
	if _, err := svc.OpenFindings(t.Context()); err == nil {
		t.Errorf("OpenFindings() on a closed store error = nil, want an error")
	}
}
