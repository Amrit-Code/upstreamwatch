package sched_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/sched"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

// These tests build a sched.Scheduler over the real pipeline.Runner and a real
// SQLite file. sched.New accepts only five-field cron, so no test waits for a
// tick; they check what is observable without one. Start and Stop run inside
// a synctest bubble, whose clock does not move while any goroutine is
// runnable, so no wall-clock minute boundary can fire a schedule mid-test.

// defaultSchedules loads a config file that sets nothing, so every schedule is
// the default from internal/config.
func defaultSchedules(t *testing.T) config.Schedules {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upw.yaml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path, func(string) string { return "" })
	if err != nil {
		t.Fatalf("config.Load(empty file) error = %v", err)
	}
	return cfg.Schedules
}

// defaultEntries returns one entry per config schedules key, each running a
// job of the same name.
func defaultEntries(t *testing.T) []sched.Entry {
	t.Helper()
	s := defaultSchedules(t)
	entries := []sched.Entry{
		{Name: "ingest", Cron: s.Ingest},
		{Name: "vulns", Cron: s.Vulns},
		{Name: "issues", Cron: s.Issues},
		{Name: "llm", Cron: s.LLM},
		{Name: "digest", Cron: s.Digest},
		{Name: "backup", Cron: s.Backup},
	}
	for i := range entries {
		entries[i].Jobs = []string{entries[i].Name}
	}
	return entries
}

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

// countingJob is a pipeline.Job that counts its runs.
type countingJob struct {
	name  string
	calls *atomic.Int64
}

func (j countingJob) Name() string { return j.name }

func (j countingJob) Run(context.Context, model.Scope) (model.Stats, error) {
	j.calls.Add(1)
	return nil, nil
}

// newRunner returns a real Runner over db with one job per entry, all counting
// into calls.
func newRunner(t *testing.T, db *store.DB, entries []sched.Entry, calls *atomic.Int64) *pipeline.Runner {
	t.Helper()
	jobs := make([]pipeline.Job, 0, len(entries))
	for _, e := range entries {
		for _, name := range e.Jobs {
			jobs = append(jobs, countingJob{name: name, calls: calls})
		}
	}
	r, err := pipeline.NewRunner(jobs, db, db, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return r
}

func TestIntegration_NewAcceptsDefaultSchedules(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	entries := defaultEntries(t)
	var calls atomic.Int64
	r := newRunner(t, db, entries, &calls)

	for _, e := range entries {
		if e.Cron == "" {
			t.Errorf("default schedule %s is empty", e.Name)
		}
	}
	if _, err := sched.New(r, entries, slog.New(slog.DiscardHandler)); err != nil {
		t.Errorf("sched.New(default schedules) error = %v, want nil", err)
	}
	// Each default on its own, so a failure names the expression.
	for _, e := range entries {
		if _, err := sched.New(r, []sched.Entry{e}, slog.New(slog.DiscardHandler)); err != nil {
			t.Errorf("sched.New(%s %q) error = %v, want nil", e.Name, e.Cron, err)
		}
	}
}

func TestIntegration_StartStopRecordsNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		entries := defaultEntries(t)
		var calls atomic.Int64
		r := newRunner(t, db, entries, &calls)
		s, err := sched.New(r, entries, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("sched.New() error = %v", err)
		}

		s.Start(t.Context())
		if err := s.Stop(t.Context()); err != nil {
			t.Errorf("Stop() error = %v, want nil", err)
		}

		runs, err := db.LatestRuns(t.Context())
		if err != nil {
			t.Fatalf("LatestRuns() error = %v", err)
		}
		if runs != nil {
			t.Errorf("LatestRuns() after Start and Stop = %+v, want no runs", runs)
		}
		if n := calls.Load(); n != 0 {
			t.Errorf("jobs ran %d times, want 0", n)
		}
		findings, err := db.ListOpenFindings(t.Context())
		if err != nil || findings != nil {
			t.Errorf("ListOpenFindings() = %+v, %v; want none", findings, err)
		}
	})
}

func TestIntegration_StopWithCancelledContextWhenIdle(t *testing.T) {
	t.Parallel()

	t.Run("before start", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			db := openDB(t)
			entries := defaultEntries(t)
			var calls atomic.Int64
			s, err := sched.New(newRunner(t, db, entries, &calls), entries, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatalf("sched.New() error = %v", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := s.Stop(ctx); err != nil {
				t.Errorf("Stop(cancelled ctx) before Start error = %v, want nil", err)
			}
		})
	})

	t.Run("after start, no chain running", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			db := openDB(t)
			entries := defaultEntries(t)
			var calls atomic.Int64
			s, err := sched.New(newRunner(t, db, entries, &calls), entries, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatalf("sched.New() error = %v", err)
			}
			s.Start(t.Context())
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			// Nothing is running, so there is nothing to wait for: Stop
			// returns nil when no chain is running, whatever the context.
			if err := s.Stop(ctx); err != nil {
				t.Errorf("Stop(cancelled ctx) after Start with no chain running error = %v, want nil", err)
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("jobs ran %d times, want 0", n)
			}
		})
	})
}
