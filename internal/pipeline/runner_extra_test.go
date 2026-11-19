package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Gap tests for WO-05: the log lines of requirement 7, UTC times (11),
// arrival order (10), the 10-second recording timeout (9), the scope reaching
// the job (3) and a caller whose context is already cancelled (10).

// logRecord is one captured log line.
type logRecord struct {
	level slog.Level
	msg   string
	keys  []string
	attrs map[string]slog.Value
}

// recordingHandler captures every record logged through it.
type recordingHandler struct {
	mu      sync.Mutex
	records []logRecord
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{level: r.Level, msg: r.Message, attrs: map[string]slog.Value{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.keys = append(rec.keys, a.Key)
		rec.attrs[a.Key] = a.Value
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) all() []logRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]logRecord(nil), h.records...)
}

// newLoggedRunner builds a Runner over fresh fakes, logging into the returned
// handler, with a clock fixed at t0.
func newLoggedRunner(t *testing.T, jobs ...*fakeJob) (*Runner, *recordingHandler, *fakeRunStore, *fakeFindingStore) {
	t.Helper()
	log := &callLog{}
	runs := &fakeRunStore{log: log}
	findings := &fakeFindingStore{log: log}
	h := &recordingHandler{}
	js := make([]Job, len(jobs))
	for i, j := range jobs {
		j.log = log
		js[i] = j
	}
	r, err := NewRunner(js, runs, findings, slog.New(h))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	r.now = func() time.Time { return t0 }
	return r, h, runs, findings
}

func TestRunner_LogsOneLinePerOutcome(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		job := &fakeJob{name: "issues", run: func(context.Context, model.Scope) (model.Stats, error) {
			return model.Stats{"fetched": 3, "not_modified": 2}, nil
		}}
		r, h, _, _ := newLoggedRunner(t, job)
		run, err := r.Run(t.Context(), "issues", model.Scope{})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		recs := h.all()
		if len(recs) != 1 {
			t.Fatalf("logged %d lines, want 1: %+v", len(recs), recs)
		}
		rec := recs[0]
		if rec.level != slog.LevelInfo || rec.msg != "job finished" {
			t.Errorf("log line = %v %q, want INFO %q", rec.level, rec.msg, "job finished")
		}
		want := map[string]string{
			"job":          "issues",
			"run_id":       "1",
			"duration_ms":  "0",
			"fetched":      "3",
			"not_modified": "2",
		}
		got := map[string]string{}
		for k, v := range rec.attrs {
			got[k] = v.String()
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("log attrs = %v, want %v", got, want)
		}
		if run.ID != 1 {
			t.Errorf("Run() ID = %d, want 1", run.ID)
		}
	})

	for name, fn := range map[string]func(context.Context, model.Scope) (model.Stats, error){
		"failure": func(context.Context, model.Scope) (model.Stats, error) { return nil, errors.New("upstream 502") },
		"panic":   func(context.Context, model.Scope) (model.Stats, error) { panic("upstream 502") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, h, runs, _ := newLoggedRunner(t, &fakeJob{name: "issues", run: fn})
			if _, err := r.Run(t.Context(), "issues", model.Scope{}); err == nil {
				t.Fatal("Run() error = nil, want an error")
			}
			recs := h.all()
			if len(recs) != 1 {
				t.Fatalf("logged %d lines, want exactly 1 (a failure is not logged twice): %+v", len(recs), recs)
			}
			rec := recs[0]
			if rec.level != slog.LevelError || rec.msg != "job failed" {
				t.Errorf("log line = %v %q, want ERROR %q", rec.level, rec.msg, "job failed")
			}
			wantKeys := []string{"job", "run_id", "error"}
			if !reflect.DeepEqual(rec.keys, wantKeys) {
				t.Errorf("log keys = %q, want %q", rec.keys, wantKeys)
			}
			if got := rec.attrs["job"].String(); got != "issues" {
				t.Errorf("log job = %q, want issues", got)
			}
			if got := rec.attrs["run_id"].String(); got != "1" {
				t.Errorf("log run_id = %q, want 1", got)
			}
			if got, want := rec.attrs["error"].String(), runs.finished[0].Error; got != want {
				t.Errorf("log error = %q, want the stored error %q", got, want)
			}
		})
	}
}

func TestRunner_StoreTimesAreUTC(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("UTC+5:30", 5*3600+1800)
	local := time.Date(2026, 3, 1, 17, 30, 0, 0, zone)

	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			job := &fakeJob{name: "issues", run: func(context.Context, model.Scope) (model.Stats, error) {
				if fail {
					return nil, errors.New("boom")
				}
				return nil, nil
			}}
			r, _, runs, findings := newLoggedRunner(t, job)
			r.now = func() time.Time { return local }
			_, _ = r.Run(t.Context(), "issues", model.Scope{}) // the outcome is checked through the fakes

			check := func(what string, got time.Time) {
				t.Helper()
				if got.Location() != time.UTC || !got.Equal(local) {
					t.Errorf("%s = %v (%v), want %v in UTC", what, got, got.Location(), local.UTC())
				}
			}
			if len(runs.started) != 1 || len(runs.finished) != 1 {
				t.Fatalf("StartRun/FinishRun calls = %d/%d, want 1/1", len(runs.started), len(runs.finished))
			}
			check("StartRun startedAt", runs.started[0])
			check("FinishRun FinishedAt", runs.finished[0].FinishedAt)
			if fail {
				if len(findings.upserted) != 1 {
					t.Fatalf("UpsertFinding calls = %d, want 1", len(findings.upserted))
				}
				check("UpsertFinding LastChangedAt", findings.upserted[0].LastChangedAt)
			} else {
				if len(findings.resolved) != 1 {
					t.Fatalf("ResolveFinding calls = %d, want 1", len(findings.resolved))
				}
				check("ResolveFinding at", findings.resolved[0].at)
			}
		})
	}
}

func TestRunner_SerialisesInArrivalOrder(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan struct{})
		var mu sync.Mutex
		var order []string
		record := func(name string) func(context.Context, model.Scope) (model.Stats, error) {
			return func(context.Context, model.Scope) (model.Stats, error) {
				mu.Lock()
				order = append(order, name)
				mu.Unlock()
				return nil, nil
			}
		}
		blocker := &fakeJob{name: "slow", run: func(context.Context, model.Scope) (model.Stats, error) {
			close(started)
			<-release
			return nil, nil
		}}
		f := newFixture(t, blocker, &fakeJob{name: "a", run: record("a")}, &fakeJob{name: "b", run: record("b")}, &fakeJob{name: "c", run: record("c")})

		var wg sync.WaitGroup
		wg.Go(func() { _, _ = f.runner.Run(t.Context(), "slow", model.Scope{}) })
		<-started
		// Queue c, a, b one at a time, each blocked on the run lock before the
		// next arrives, so arrival order is known.
		for _, name := range []string{"c", "a", "b"} {
			wg.Go(func() {
				if _, err := f.runner.Run(t.Context(), name, model.Scope{}); err != nil {
					t.Errorf("Run(%s) error = %v", name, err)
				}
			})
			synctest.Wait()
		}
		close(release)
		wg.Wait()

		if want := []string{"c", "a", "b"}; !reflect.DeepEqual(order, want) {
			t.Errorf("jobs ran in order %q, want arrival order %q", order, want)
		}
	})
}

type ctxKey struct{}

// recordCtx is what a store call saw of its context.
type recordCtx struct {
	call     string
	err      error
	deadline time.Duration // deadline minus the time of the call; 0 if none
	value    any
}

// ctxRunStore and ctxFindingStore record the context each call receives.
type ctxRunStore struct {
	fakeRunStore
	seen *[]recordCtx
}

func (s *ctxRunStore) FinishRun(ctx context.Context, run model.Run) error {
	*s.seen = append(*s.seen, describe("FinishRun", ctx))
	return s.fakeRunStore.FinishRun(ctx, run)
}

type ctxFindingStore struct {
	fakeFindingStore
	seen *[]recordCtx
}

func (s *ctxFindingStore) UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error) {
	*s.seen = append(*s.seen, describe("UpsertFinding", ctx))
	return s.fakeFindingStore.UpsertFinding(ctx, f)
}

func (s *ctxFindingStore) ResolveFinding(ctx context.Context, fp string, at time.Time) error {
	*s.seen = append(*s.seen, describe("ResolveFinding", ctx))
	return s.fakeFindingStore.ResolveFinding(ctx, fp, at)
}

func describe(call string, ctx context.Context) recordCtx {
	rc := recordCtx{call: call, err: ctx.Err(), value: ctx.Value(ctxKey{})}
	if dl, ok := ctx.Deadline(); ok {
		rc.deadline = dl.Sub(time.Now())
	}
	return rc
}

func TestRunner_RecordingUsesDetachedTenSecondContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cancel   bool // cancel the caller's context while the job runs
		fail     bool
		wantCall []string
	}{
		{name: "success", wantCall: []string{"FinishRun", "ResolveFinding"}},
		{name: "failure", fail: true, wantCall: []string{"FinishRun", "UpsertFinding"}},
		{name: "cancelled", cancel: true, wantCall: []string{"FinishRun", "UpsertFinding"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Inside the bubble time stands still while the Runner computes,
			// so the remaining time is exactly the timeout.
			synctest.Test(t, func(t *testing.T) {
				var seen []recordCtx
				log := &callLog{}
				runs := &ctxRunStore{fakeRunStore: fakeRunStore{log: log}, seen: &seen}
				findings := &ctxFindingStore{fakeFindingStore: fakeFindingStore{log: log}, seen: &seen}
				ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxKey{}, "request-7"))
				defer cancel()
				job := &fakeJob{name: "issues", log: log, run: func(ctx context.Context, _ model.Scope) (model.Stats, error) {
					if tt.cancel {
						cancel()
						<-ctx.Done()
						return nil, ctx.Err()
					}
					if tt.fail {
						return nil, errors.New("boom")
					}
					return nil, nil
				}}
				r, err := NewRunner([]Job{job}, runs, findings, slog.New(slog.DiscardHandler))
				if err != nil {
					t.Fatalf("NewRunner() error = %v", err)
				}
				_, _ = r.Run(ctx, "issues", model.Scope{}) // outcome checked through the fakes

				var calls []string
				for _, s := range seen {
					calls = append(calls, s.call)
					if s.err != nil {
						t.Errorf("%s ctx.Err() = %v, want nil", s.call, s.err)
					}
					if s.deadline != 10*time.Second {
						t.Errorf("%s deadline in %v, want 10s", s.call, s.deadline)
					}
					if s.value != "request-7" {
						t.Errorf("%s ctx value = %v, want the caller's value request-7", s.call, s.value)
					}
				}
				if !reflect.DeepEqual(calls, tt.wantCall) {
					t.Errorf("recording calls = %q, want %q", calls, tt.wantCall)
				}
			})
		})
	}
}

func TestRunner_PassesScopeAndContextToJob(t *testing.T) {
	t.Parallel()
	for _, scope := range []model.Scope{{}, {RepoSlug: "example-server"}} {
		t.Run(scope.RepoSlug, func(t *testing.T) {
			t.Parallel()
			var gotScope model.Scope
			var gotValue any
			job := &fakeJob{name: "timeline", run: func(ctx context.Context, s model.Scope) (model.Stats, error) {
				gotScope, gotValue = s, ctx.Value(ctxKey{})
				return nil, nil
			}}
			f := newFixture(t, job)
			ctx := context.WithValue(t.Context(), ctxKey{}, "trace-1")
			if _, err := f.runner.Run(ctx, "timeline", scope); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if gotScope != scope {
				t.Errorf("job scope = %+v, want %+v", gotScope, scope)
			}
			if gotValue != "trace-1" {
				t.Errorf("job ctx value = %v, want the caller's context", gotValue)
			}
		})
	}
}

func TestRunner_AlreadyCancelledContextStartsNothing(t *testing.T) {
	t.Parallel()
	job := &fakeJob{name: "issues"}
	f := newFixture(t, job)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := f.runner.Run(ctx, "issues", model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run(cancelled ctx) error = %v, want errors.Is context.Canceled", err)
	}
	if !reflect.DeepEqual(got, model.Run{}) {
		t.Errorf("Run(cancelled ctx) = %+v, want zero run", got)
	}
	if calls := f.log.get(); len(calls) != 0 {
		t.Errorf("calls = %q, want none", calls)
	}

	// The lock is free afterwards: a live context runs normally.
	if _, err := f.runner.Run(t.Context(), "issues", model.Scope{}); err != nil {
		t.Errorf("Run() after a cancelled caller error = %v, want nil", err)
	}
}
