package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

var (
	t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(1500 * time.Millisecond)
)

// callLog records store and job calls in order, shared by the fakes of one test.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(c string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, c)
}

func (l *callLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

type fakeJob struct {
	name  string
	log   *callLog
	run   func(ctx context.Context, scope model.Scope) (model.Stats, error)
	calls atomic.Int64
}

func (j *fakeJob) Name() string { return j.name }

func (j *fakeJob) Run(ctx context.Context, scope model.Scope) (model.Stats, error) {
	j.calls.Add(1)
	if j.log != nil {
		j.log.add("job.Run " + j.name)
	}
	if j.run == nil {
		return nil, nil
	}
	return j.run(ctx, scope)
}

type fakeRunStore struct {
	log       *callLog
	mu        sync.Mutex
	nextID    int64
	startErr  error
	finishErr error
	started   []time.Time
	finished  []model.Run
	ctxErrs   []error // ctx.Err() seen by FinishRun
}

func (s *fakeRunStore) StartRun(ctx context.Context, job string, startedAt time.Time) (model.Run, error) {
	s.log.add("StartRun " + job)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = append(s.started, startedAt)
	if s.startErr != nil {
		return model.Run{}, s.startErr
	}
	s.nextID++
	return model.Run{ID: s.nextID, Job: job, StartedAt: startedAt, Status: model.RunRunning}, nil
}

func (s *fakeRunStore) FinishRun(ctx context.Context, run model.Run) error {
	s.log.add("FinishRun " + run.Job)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished = append(s.finished, run)
	s.ctxErrs = append(s.ctxErrs, ctx.Err())
	return s.finishErr
}

type resolveCall struct {
	fingerprint string
	at          time.Time
}

type fakeFindingStore struct {
	log        *callLog
	mu         sync.Mutex
	upsertErr  error
	resolveErr error
	upserted   []model.Finding
	resolved   []resolveCall
	ctxErrs    []error
}

func (s *fakeFindingStore) UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error) {
	s.log.add("UpsertFinding " + f.Fingerprint)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upserted = append(s.upserted, f)
	s.ctxErrs = append(s.ctxErrs, ctx.Err())
	if s.upsertErr != nil {
		return model.Finding{}, s.upsertErr
	}
	f.ID = 1
	return f, nil
}

func (s *fakeFindingStore) ResolveFinding(ctx context.Context, fingerprint string, at time.Time) error {
	s.log.add("ResolveFinding " + fingerprint)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolved = append(s.resolved, resolveCall{fingerprint, at})
	s.ctxErrs = append(s.ctxErrs, ctx.Err())
	return s.resolveErr
}

type fixture struct {
	log      *callLog
	runs     *fakeRunStore
	findings *fakeFindingStore
	runner   *Runner
}

// newFixture builds a Runner over fakes whose clock returns t0, then t1, then
// t1 for every later call.
func newFixture(t *testing.T, jobs ...*fakeJob) *fixture {
	t.Helper()
	log := &callLog{}
	f := &fixture{
		log:      log,
		runs:     &fakeRunStore{log: log},
		findings: &fakeFindingStore{log: log},
	}
	js := make([]Job, len(jobs))
	for i, j := range jobs {
		if j.log == nil {
			j.log = log
		}
		js[i] = j
	}
	r, err := NewRunner(js, f.runs, f.findings, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	var mu sync.Mutex
	times := []time.Time{t0, t1}
	r.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		got := times[0]
		if len(times) > 1 {
			times = times[1:]
		}
		return got
	}
	f.runner = r
	return f
}

func TestRunner_Success(t *testing.T) {
	t.Parallel()
	stats := model.Stats{"fetched": 3, "not_modified": 2}
	job := &fakeJob{name: "issues", run: func(context.Context, model.Scope) (model.Stats, error) {
		return stats, nil
	}}
	f := newFixture(t, job)

	got, err := f.runner.Run(t.Context(), "issues", model.Scope{RepoSlug: "example-org/example-server"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := model.Run{ID: 1, Job: "issues", StartedAt: t0, FinishedAt: t1, Status: model.RunSucceeded, Stats: stats}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Run() = %+v, want %+v", got, want)
	}
	if len(f.runs.finished) != 1 || !reflect.DeepEqual(f.runs.finished[0], want) {
		t.Errorf("FinishRun got %+v, want [%+v]", f.runs.finished, want)
	}
	if !reflect.DeepEqual(f.runs.started, []time.Time{t0}) {
		t.Errorf("StartRun times = %v, want [%v]", f.runs.started, t0)
	}
	wantResolved := []resolveCall{{"job_failure|issues", t1}}
	if !reflect.DeepEqual(f.findings.resolved, wantResolved) {
		t.Errorf("ResolveFinding calls = %v, want %v", f.findings.resolved, wantResolved)
	}
	if len(f.findings.upserted) != 0 {
		t.Errorf("UpsertFinding called %d times, want 0", len(f.findings.upserted))
	}
	wantCalls := []string{"StartRun issues", "job.Run issues", "FinishRun issues", "ResolveFinding job_failure|issues"}
	if got := f.log.get(); !reflect.DeepEqual(got, wantCalls) {
		t.Errorf("calls = %q, want %q", got, wantCalls)
	}
}

func TestRunner_Failure(t *testing.T) {
	t.Parallel()
	jobErr := errors.New("poll example-org/example-server issues: 502 bad gateway")
	partial := model.Stats{"fetched": 1}
	job := &fakeJob{name: "issues", run: func(context.Context, model.Scope) (model.Stats, error) {
		return partial, jobErr
	}}
	f := newFixture(t, job)

	got, err := f.runner.Run(t.Context(), "issues", model.Scope{})
	if !errors.Is(err, jobErr) {
		t.Fatalf("Run() error = %v, want errors.Is %v", err, jobErr)
	}
	if want := "run job issues: " + jobErr.Error(); err.Error() != want {
		t.Errorf("Run() error text = %q, want %q", err.Error(), want)
	}

	wantRun := model.Run{ID: 1, Job: "issues", StartedAt: t0, FinishedAt: t1, Status: model.RunFailed, Stats: partial, Error: jobErr.Error()}
	if !reflect.DeepEqual(got, wantRun) {
		t.Errorf("Run() = %+v, want %+v", got, wantRun)
	}
	if len(f.runs.finished) != 1 || !reflect.DeepEqual(f.runs.finished[0], wantRun) {
		t.Errorf("FinishRun got %+v, want [%+v]", f.runs.finished, wantRun)
	}
	wantFinding := model.Finding{
		Kind:          model.KindJobFailure,
		Fingerprint:   "job_failure|issues",
		Score:         50,
		RankReason:    "job issues failed: " + jobErr.Error(),
		LastChangedAt: t1,
	}
	if len(f.findings.upserted) != 1 || !reflect.DeepEqual(f.findings.upserted[0], wantFinding) {
		t.Errorf("UpsertFinding got %+v, want [%+v]", f.findings.upserted, wantFinding)
	}
	if len(f.findings.resolved) != 0 {
		t.Errorf("ResolveFinding called %d times, want 0", len(f.findings.resolved))
	}
	wantCalls := []string{"StartRun issues", "job.Run issues", "FinishRun issues", "UpsertFinding job_failure|issues"}
	if got := f.log.get(); !reflect.DeepEqual(got, wantCalls) {
		t.Errorf("calls = %q, want %q", got, wantCalls)
	}
}

func TestRunner_Panic(t *testing.T) {
	t.Parallel()
	job := &fakeJob{name: "timeline", run: func(context.Context, model.Scope) (model.Stats, error) {
		panic("index out of range")
	}}
	f := newFixture(t, job)

	got, err := f.runner.Run(t.Context(), "timeline", model.Scope{})
	if err == nil {
		t.Fatal("Run() error = nil, want an error")
	}
	if want := "run job timeline: panic: index out of range"; err.Error() != want {
		t.Errorf("Run() error = %q, want %q", err.Error(), want)
	}
	if got.Status != model.RunFailed || !strings.HasPrefix(got.Error, "panic: ") {
		t.Errorf("Run() = status %q error %q, want %q and prefix %q", got.Status, got.Error, model.RunFailed, "panic: ")
	}
	if len(f.runs.finished) != 1 || f.runs.finished[0].Status != model.RunFailed || f.runs.finished[0].Error != "panic: index out of range" {
		t.Errorf("FinishRun got %+v, want one failed run with the panic text", f.runs.finished)
	}
	if len(f.findings.upserted) != 1 || f.findings.upserted[0].RankReason != "job timeline failed: panic: index out of range" {
		t.Errorf("UpsertFinding got %+v, want one finding for the panic", f.findings.upserted)
	}
}

func TestRunner_UnknownJob(t *testing.T) {
	t.Parallel()
	job := &fakeJob{name: "issues"}
	f := newFixture(t, job)

	got, err := f.runner.Run(t.Context(), "nope", model.Scope{})
	if !errors.Is(err, ErrUnknownJob) {
		t.Fatalf("Run() error = %v, want errors.Is ErrUnknownJob", err)
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("Run() error = %q, want it to name the job", err.Error())
	}
	if !reflect.DeepEqual(got, model.Run{}) {
		t.Errorf("Run() = %+v, want zero run", got)
	}
	if calls := f.log.get(); len(calls) != 0 {
		t.Errorf("calls = %q, want none", calls)
	}
}

func TestNewRunner_DuplicateAndEmptyName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		jobs    []Job
		wantDup bool
		wantSub string
	}{
		{"duplicate", []Job{&fakeJob{name: "issues"}, &fakeJob{name: "timeline"}, &fakeJob{name: "issues"}}, true, "issues"},
		{"empty name", []Job{&fakeJob{name: "issues"}, &fakeJob{name: ""}}, false, "empty job name"},
		{"nil job", []Job{nil}, false, "nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := NewRunner(tt.jobs, &fakeRunStore{}, &fakeFindingStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err == nil {
				t.Fatalf("NewRunner() = %v, nil error; want an error", r)
			}
			if got := errors.Is(err, ErrDuplicateJob); got != tt.wantDup {
				t.Errorf("errors.Is(%v, ErrDuplicateJob) = %v, want %v", err, got, tt.wantDup)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("NewRunner() error = %q, want it to contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}

func TestRunner_Jobs(t *testing.T) {
	t.Parallel()
	f := newFixture(t, &fakeJob{name: "ingest"}, &fakeJob{name: "timeline"}, &fakeJob{name: "issues"})
	want := []string{"ingest", "timeline", "issues"}
	got := f.runner.Jobs()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Jobs() = %q, want %q", got, want)
	}
	got[0] = "changed"
	if again := f.runner.Jobs(); !reflect.DeepEqual(again, want) {
		t.Errorf("Jobs() after caller edit = %q, want %q", again, want)
	}
}

func TestRunner_StartRunFails(t *testing.T) {
	t.Parallel()
	job := &fakeJob{name: "issues"}
	f := newFixture(t, job)
	storeErr := errors.New("database is locked")
	f.runs.startErr = storeErr

	_, err := f.runner.Run(t.Context(), "issues", model.Scope{})
	if !errors.Is(err, storeErr) {
		t.Fatalf("Run() error = %v, want errors.Is %v", err, storeErr)
	}
	if n := job.calls.Load(); n != 0 {
		t.Errorf("job called %d times, want 0", n)
	}
	wantCalls := []string{"StartRun issues"}
	if got := f.log.get(); !reflect.DeepEqual(got, wantCalls) {
		t.Errorf("calls = %q, want %q", got, wantCalls)
	}
}

func TestRunner_FinishRunFailsStillRaisesFinding(t *testing.T) {
	t.Parallel()
	jobErr := errors.New("job broke")
	finishErr := errors.New("finish broke")
	upsertErr := errors.New("upsert broke")
	resolveErr := errors.New("resolve broke")

	t.Run("failed job", func(t *testing.T) {
		t.Parallel()
		job := &fakeJob{name: "issues", run: func(context.Context, model.Scope) (model.Stats, error) { return nil, jobErr }}
		f := newFixture(t, job)
		f.runs.finishErr = finishErr
		f.findings.upsertErr = upsertErr

		_, err := f.runner.Run(t.Context(), "issues", model.Scope{})
		for _, want := range []error{jobErr, finishErr, upsertErr} {
			if !errors.Is(err, want) {
				t.Errorf("Run() error = %v, want errors.Is %v", err, want)
			}
		}
		if err != nil && !strings.HasPrefix(err.Error(), "run job issues: job broke\n") {
			t.Errorf("Run() error = %q, want the job error first", err.Error())
		}
		if len(f.findings.upserted) != 1 {
			t.Errorf("UpsertFinding called %d times, want 1", len(f.findings.upserted))
		}
	})

	t.Run("succeeded job", func(t *testing.T) {
		t.Parallel()
		job := &fakeJob{name: "issues"}
		f := newFixture(t, job)
		f.runs.finishErr = finishErr
		f.findings.resolveErr = resolveErr

		_, err := f.runner.Run(t.Context(), "issues", model.Scope{})
		for _, want := range []error{finishErr, resolveErr} {
			if !errors.Is(err, want) {
				t.Errorf("Run() error = %v, want errors.Is %v", err, want)
			}
		}
		if len(f.findings.resolved) != 1 {
			t.Errorf("ResolveFinding called %d times, want 1", len(f.findings.resolved))
		}
	})
}

func TestRunner_CancelledContextStillRecords(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	job := &fakeJob{name: "issues", run: func(ctx context.Context, _ model.Scope) (model.Stats, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	f := newFixture(t, job)

	ctx, cancel := context.WithCancel(t.Context())
	type result struct {
		run model.Run
		err error
	}
	done := make(chan result, 1)
	go func() {
		run, err := f.runner.Run(ctx, "issues", model.Scope{})
		done <- result{run, err}
	}()
	<-started
	cancel()
	res := <-done

	if !errors.Is(res.err, context.Canceled) {
		t.Errorf("Run() error = %v, want errors.Is context.Canceled", res.err)
	}
	if res.run.Status != model.RunFailed || res.run.Error != context.Canceled.Error() {
		t.Errorf("Run() = status %q error %q, want %q %q", res.run.Status, res.run.Error, model.RunFailed, context.Canceled.Error())
	}
	if len(f.runs.ctxErrs) != 1 || f.runs.ctxErrs[0] != nil {
		t.Errorf("FinishRun ctx errors = %v, want [<nil>]", f.runs.ctxErrs)
	}
	if len(f.findings.ctxErrs) != 1 || f.findings.ctxErrs[0] != nil {
		t.Errorf("UpsertFinding ctx errors = %v, want [<nil>]", f.findings.ctxErrs)
	}
	if len(f.findings.upserted) != 1 {
		t.Errorf("UpsertFinding called %d times, want 1", len(f.findings.upserted))
	}
}

func TestRunner_ContextEndedJobReturnsNil(t *testing.T) {
	t.Parallel()
	partial := model.Stats{"fetched": 2}
	tests := []struct {
		name string
		// setup returns the context to run with and the job body; the context
		// ends while the job runs and the job then returns a nil error.
		setup   func(t *testing.T) (context.Context, func(context.Context, model.Scope) (model.Stats, error))
		wantErr error
	}{
		{
			name: "cancelled",
			setup: func(t *testing.T) (context.Context, func(context.Context, model.Scope) (model.Stats, error)) {
				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(cancel)
				return ctx, func(ctx context.Context, _ model.Scope) (model.Stats, error) {
					cancel()
					<-ctx.Done()
					return partial, nil
				}
			},
			wantErr: context.Canceled,
		},
		{
			name: "deadline",
			setup: func(t *testing.T) (context.Context, func(context.Context, model.Scope) (model.Stats, error)) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				t.Cleanup(cancel)
				return ctx, func(ctx context.Context, _ model.Scope) (model.Stats, error) {
					<-ctx.Done()
					return partial, nil
				}
			},
			wantErr: context.DeadlineExceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// synctest's fake clock lets the one-minute deadline pass at once.
			synctest.Test(t, func(t *testing.T) {
				ctx, body := tt.setup(t)
				job := &fakeJob{name: "issues", run: body}
				f := newFixture(t, job)

				got, err := f.runner.Run(ctx, "issues", model.Scope{})

				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Run() error = %v, want errors.Is %v", err, tt.wantErr)
				}
				if got.Status != model.RunFailed || got.Error != tt.wantErr.Error() {
					t.Errorf("Run() = status %q error %q, want %q %q", got.Status, got.Error, model.RunFailed, tt.wantErr.Error())
				}
				if len(f.runs.finished) != 1 || f.runs.finished[0].Status != model.RunFailed || f.runs.finished[0].Error != tt.wantErr.Error() {
					t.Errorf("FinishRun got %+v, want one failed run with %q", f.runs.finished, tt.wantErr.Error())
				}
				wantFinding := model.Finding{
					Kind:          model.KindJobFailure,
					Fingerprint:   "job_failure|issues",
					Score:         50,
					RankReason:    "job issues failed: " + tt.wantErr.Error(),
					LastChangedAt: t1,
				}
				if len(f.findings.upserted) != 1 || !reflect.DeepEqual(f.findings.upserted[0], wantFinding) {
					t.Errorf("UpsertFinding got %+v, want [%+v]", f.findings.upserted, wantFinding)
				}
				if len(f.findings.resolved) != 0 {
					t.Errorf("ResolveFinding called %d times, want 0", len(f.findings.resolved))
				}
			})
		})
	}
}

func TestRunner_CancelledWhileWaiting(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	started := make(chan struct{})
	blocker := &fakeJob{name: "slow", run: func(context.Context, model.Scope) (model.Stats, error) {
		close(started)
		<-release
		return nil, nil
	}}
	waiter := &fakeJob{name: "fast"}
	f := newFixture(t, blocker, waiter)

	var wg sync.WaitGroup
	wg.Go(func() {
		if _, err := f.runner.Run(t.Context(), "slow", model.Scope{}); err != nil {
			t.Errorf("Run(slow) error = %v", err)
		}
	})
	<-started

	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := f.runner.Run(ctx, "fast", model.Scope{})
		errc <- err
	}()
	cancel()
	err := <-errc
	close(release)
	wg.Wait()

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run(fast) error = %v, want errors.Is context.Canceled", err)
	}
	if n := waiter.calls.Load(); n != 0 {
		t.Errorf("fast job called %d times, want 0", n)
	}
	if len(f.runs.started) != 1 {
		t.Errorf("StartRun called %d times, want 1", len(f.runs.started))
	}
}

func TestRunner_SerialisesRuns(t *testing.T) {
	t.Parallel()
	var active, overlaps atomic.Int64
	work := func(context.Context, model.Scope) (model.Stats, error) {
		if active.Add(1) > 1 {
			overlaps.Add(1)
		}
		for range 1000 {
			if active.Load() > 1 {
				overlaps.Add(1)
			}
		}
		active.Add(-1)
		return nil, nil
	}
	a := &fakeJob{name: "a", run: work}
	b := &fakeJob{name: "b", run: work}
	f := newFixture(t, a, b)

	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "a", "b"} {
		wg.Go(func() {
			if _, err := f.runner.Run(t.Context(), name, model.Scope{}); err != nil {
				t.Errorf("Run(%s) error = %v", name, err)
			}
		})
	}
	wg.Wait()

	if n := overlaps.Load(); n != 0 {
		t.Errorf("overlapping runs observed %d times, want 0", n)
	}
	if n := a.calls.Load() + b.calls.Load(); n != 4 {
		t.Errorf("jobs called %d times, want 4", n)
	}
}

func TestRunner_LongErrorTruncated(t *testing.T) {
	t.Parallel()
	// 3-byte runes after a 1-byte offset, so byte 500 falls inside a rune.
	long := "x" + strings.Repeat("€", 700)
	if len(long) < 2000 {
		t.Fatalf("test error is %d bytes, want at least 2000", len(long))
	}
	job := &fakeJob{name: "issues", run: func(context.Context, model.Scope) (model.Stats, error) {
		return nil, errors.New(long)
	}}
	f := newFixture(t, job)

	if _, err := f.runner.Run(t.Context(), "issues", model.Scope{}); err == nil {
		t.Fatal("Run() error = nil, want an error")
	}
	if len(f.findings.upserted) != 1 {
		t.Fatalf("UpsertFinding called %d times, want 1", len(f.findings.upserted))
	}
	reason := f.findings.upserted[0].RankReason
	prefix := "job issues failed: "
	if !strings.HasPrefix(reason, prefix) {
		t.Fatalf("RankReason = %q, want prefix %q", reason, prefix)
	}
	part := strings.TrimPrefix(reason, prefix)
	if len(part) > 500 || len(part) < 497 {
		t.Errorf("RankReason error part is %d bytes, want 497 to 500", len(part))
	}
	if !utf8.ValidString(reason) {
		t.Errorf("RankReason is not valid UTF-8")
	}
	if !strings.HasPrefix(long, part) {
		t.Errorf("RankReason error part is not a prefix of the error text")
	}
	if f.runs.finished[0].Error != long {
		t.Errorf("FinishRun error is %d bytes, want the full %d", len(f.runs.finished[0].Error), len(long))
	}
}

func TestTruncateUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exact", 5, "exact"},
		{"abcdef", 3, "abc"},
		{"a€b", 2, "a"},
		{"a€b", 4, "a€"},
		{"€", 0, ""},
	}
	for _, tt := range tests {
		if got := truncateUTF8(tt.in, tt.n); got != tt.want {
			t.Errorf("truncateUTF8(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestRunner_RedactsJobError(t *testing.T) {
	t.Parallel()
	token := "github_pat_" + strings.Repeat("A1b", 10)
	jobErr := errors.New("fetch https://u:hunter2@git.example/x: " + token)
	job := &fakeJob{name: "ingest", run: func(context.Context, model.Scope) (model.Stats, error) {
		return nil, jobErr
	}}
	f := newFixture(t, job)
	var logOut bytes.Buffer
	f.runner.logger = slog.New(slog.NewTextHandler(&logOut, nil))

	_, err := f.runner.Run(t.Context(), "ingest", model.Scope{})
	if err == nil {
		t.Fatal("Run() error = nil, want an error")
	}
	if len(f.runs.finished) != 1 || len(f.findings.upserted) != 1 {
		t.Fatalf("FinishRun calls = %d, UpsertFinding calls = %d, want 1 each", len(f.runs.finished), len(f.findings.upserted))
	}

	places := map[string]string{
		"runs error":    f.runs.finished[0].Error,
		"rank reason":   f.findings.upserted[0].RankReason,
		"log output":    logOut.String(),
		"returned text": err.Error(),
	}
	for place, text := range places {
		for _, secret := range []string{"hunter2", token} {
			if strings.Contains(text, secret) {
				t.Errorf("%s = %q, contains secret %q", place, text, secret)
			}
		}
		if !strings.Contains(text, "[REDACTED]") {
			t.Errorf("%s = %q, want it to contain [REDACTED]", place, text)
		}
	}
	if want := "run job ingest: fetch https://[REDACTED]@git.example/x: [REDACTED]"; err.Error() != want {
		t.Errorf("Run() error text = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, jobErr) {
		t.Errorf("Run() error = %v, want errors.Is the job error", err)
	}
}

func TestRunner_RedactedErrorUnwraps(t *testing.T) {
	t.Parallel()
	job := &fakeJob{name: "ingest", run: func(context.Context, model.Scope) (model.Stats, error) {
		return nil, fmt.Errorf("clone https://u:hunter2@git.example/x: %w", context.Canceled)
	}}
	f := newFixture(t, job)

	_, err := f.runner.Run(t.Context(), "ingest", model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(%v, context.Canceled) = false, want true", err)
	}
	if want := "run job ingest: clone https://[REDACTED]@git.example/x: context canceled"; err == nil || err.Error() != want {
		t.Errorf("Run() error = %v, want %q", err, want)
	}
}

func TestJobFailureFingerprint(t *testing.T) {
	t.Parallel()
	tests := []struct{ job, want string }{
		{"issues", "job_failure|issues"},
		{"timeline", "job_failure|timeline"},
		{"", "job_failure|"},
	}
	for _, tt := range tests {
		if got := JobFailureFingerprint(tt.job); got != tt.want {
			t.Errorf("JobFailureFingerprint(%q) = %q, want %q", tt.job, got, tt.want)
		}
	}
}
