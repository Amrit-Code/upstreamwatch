package sched

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// fakeRunner records every Run call; fn, when set, decides what each job
// does (block, fail, panic, cancel).
type fakeRunner struct {
	mu     sync.Mutex
	calls  []string
	scopes []model.Scope
	fn     func(ctx context.Context, job string) error
}

func (f *fakeRunner) Run(ctx context.Context, job string, scope model.Scope) (model.Run, error) {
	f.mu.Lock()
	f.calls = append(f.calls, job)
	f.scopes = append(f.scopes, scope)
	f.mu.Unlock()
	if f.fn == nil {
		return model.Run{Job: job}, nil
	}
	return model.Run{Job: job}, f.fn(ctx, job)
}

func (f *fakeRunner) gotCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// logRecord is one captured log line with its attributes as strings.
type logRecord struct {
	level slog.Level
	msg   string
	keys  []string
	attrs map[string]string
}

// logSink captures records from every logger built on it.
type logSink struct {
	mu      sync.Mutex
	records []logRecord
}

func (s *logSink) Enabled(context.Context, slog.Level) bool { return true }

func (s *logSink) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{level: r.Level, msg: r.Message, attrs: map[string]string{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.keys = append(rec.keys, a.Key)
		rec.attrs[a.Key] = a.Value.String()
		return true
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

func (s *logSink) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *logSink) WithGroup(string) slog.Handler      { return s }

func (s *logSink) all() []logRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]logRecord(nil), s.records...)
}

func (s *logSink) withMsg(msg string) []logRecord {
	var out []logRecord
	for _, r := range s.all() {
		if r.msg == msg {
			out = append(out, r)
		}
	}
	return out
}

func newSink() (*logSink, *slog.Logger) {
	s := &logSink{}
	return s, slog.New(s)
}

// every is a constant-interval schedule, used to tick faster than cron's
// one-minute resolution.
type every time.Duration

func (e every) Next(t time.Time) time.Time { return t.Add(time.Duration(e)) }

// parseDuration reads the expression as a Go duration, for real sub-second
// ticking through newScheduler.
func parseDuration(expr string) (cron.Schedule, error) {
	d, err := time.ParseDuration(expr)
	if err != nil {
		return nil, err
	}
	return every(d), nil
}

func mustNew(t *testing.T, r Runner, entries []Entry, logger *slog.Logger) *Scheduler {
	t.Helper()
	s, err := New(r, entries, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return s
}

func TestNew_Validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries []Entry
		want    []string // substrings, one per joined error; empty means valid
	}{
		{
			name: "valid entries including one with no jobs",
			entries: []Entry{
				{Name: "ingest", Cron: "5 */6 * * *", Jobs: []string{"ingest", "timeline"}},
				{Name: "digest", Cron: "0 8 * * 1", Jobs: []string{"digest"}},
				{Name: "idle", Cron: "30 3 * * *"},
			},
		},
		{
			name:    "no entries",
			entries: nil,
		},
		{
			name:    "empty name",
			entries: []Entry{{Name: "", Cron: "0 8 * * *", Jobs: []string{"digest"}}},
			want:    []string{"schedule at index 0: empty name"},
		},
		{
			name: "duplicate name",
			entries: []Entry{
				{Name: "ingest", Cron: "0 8 * * *", Jobs: []string{"a"}},
				{Name: "ingest", Cron: "0 9 * * *", Jobs: []string{"b"}},
			},
			want: []string{"schedule ingest: duplicate name"},
		},
		{
			name:    "six fields",
			entries: []Entry{{Name: "x", Cron: "* * * * * *", Jobs: []string{"a"}}},
			want:    []string{`schedule x: parse cron expression "* * * * * *"`},
		},
		{
			name:    "every descriptor",
			entries: []Entry{{Name: "x", Cron: "@every 1s", Jobs: []string{"a"}}},
			want:    []string{`schedule x: parse cron expression "@every 1s"`},
		},
		{
			name:    "daily descriptor",
			entries: []Entry{{Name: "x", Cron: "@daily", Jobs: []string{"a"}}},
			want:    []string{`schedule x: parse cron expression "@daily"`},
		},
		{
			name:    "time zone prefix",
			entries: []Entry{{Name: "x", Cron: "CRON_TZ=UTC 0 8 * * *", Jobs: []string{"a"}}},
			want:    []string{`schedule x: parse cron expression "CRON_TZ=UTC 0 8 * * *"`},
		},
		{
			name:    "time zone prefix alone",
			entries: []Entry{{Name: "x", Cron: "TZ=UTC", Jobs: []string{"a"}}},
			want:    []string{`schedule x: parse cron expression "TZ=UTC"`},
		},
		{
			name:    "empty expression",
			entries: []Entry{{Name: "x", Cron: "", Jobs: []string{"a"}}},
			want:    []string{`schedule x: parse cron expression ""`},
		},
		{
			name:    "out of range field",
			entries: []Entry{{Name: "x", Cron: "61 * * * *", Jobs: []string{"a"}}},
			want:    []string{`schedule x: parse cron expression "61 * * * *"`},
		},
		{
			name:    "empty job name",
			entries: []Entry{{Name: "x", Cron: "0 8 * * *", Jobs: []string{"a", ""}}},
			want:    []string{"schedule x: job 1: empty job name"},
		},
		{
			name: "several problems reported together",
			entries: []Entry{
				{Name: "", Cron: "0 8 * * *", Jobs: []string{"a"}},
				{Name: "ok", Cron: "0 8 * * *", Jobs: []string{"a"}},
				{Name: "ok", Cron: "@every 1s", Jobs: []string{""}},
			},
			want: []string{
				"schedule at index 0: empty name",
				"schedule ok: duplicate name",
				`schedule ok: parse cron expression "@every 1s"`,
				"schedule ok: job 0: empty job name",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sink, logger := newSink()
			r := &fakeRunner{}
			s, err := New(r, tt.entries, logger)
			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("New() error = %v, want nil", err)
				}
				if s == nil {
					t.Fatal("New() = nil Scheduler, want non-nil")
				}
			} else {
				if err == nil {
					t.Fatalf("New() error = nil, want errors containing %q", tt.want)
				}
				joined, ok := err.(interface{ Unwrap() []error })
				if !ok {
					t.Fatalf("New() error %T is not a joined error", err)
				}
				if got := len(joined.Unwrap()); got != len(tt.want) {
					t.Errorf("New() joined %d errors, want %d: %v", got, len(tt.want), err)
				}
				for _, w := range tt.want {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("New() error = %q, want it to contain %q", err, w)
					}
				}
			}
			if got := len(sink.all()); got != 0 {
				t.Errorf("New() logged %d lines, want 0", got)
			}
			if got := r.gotCalls(); len(got) != 0 {
				t.Errorf("New() made Runner calls %v, want none", got)
			}
		})
	}
}

func TestStart_LogsOneLinePerEntry(t *testing.T) {
	t.Parallel()
	sink, logger := newSink()
	entries := []Entry{
		{Name: "ingest", Cron: "0 */6 * * *", Jobs: []string{"ingest", "timeline", "usage"}},
		{Name: "digest", Cron: "0 8 * * *", Jobs: []string{"digest"}},
		{Name: "idle", Cron: "30 3 * * *"},
	}
	s := mustNew(t, &fakeRunner{}, entries, logger)
	s.Start(t.Context())
	s.Start(t.Context()) // second call is a no-op
	if err := s.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	got := sink.withMsg("schedule registered")
	want := []map[string]string{
		{"schedule": "ingest", "cron": "0 */6 * * *", "jobs": "ingest,timeline,usage"},
		{"schedule": "digest", "cron": "0 8 * * *", "jobs": "digest"},
		{"schedule": "idle", "cron": "30 3 * * *", "jobs": ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d %q lines, want %d", len(got), "schedule registered", len(want))
	}
	for i, rec := range got {
		if rec.level != slog.LevelInfo {
			t.Errorf("line %d level = %v, want INFO", i, rec.level)
		}
		if !reflect.DeepEqual(rec.attrs, want[i]) {
			t.Errorf("line %d attrs = %v, want %v", i, rec.attrs, want[i])
		}
		if wantKeys := []string{"schedule", "cron", "jobs"}; !reflect.DeepEqual(rec.keys, wantKeys) {
			t.Errorf("line %d keys = %v, want %v", i, rec.keys, wantKeys)
		}
	}
}

type ctxKey struct{}

func TestChain_RunsJobsInOrder(t *testing.T) {
	t.Parallel()
	sink, logger := newSink()
	ctx := context.WithValue(t.Context(), ctxKey{}, "start-ctx")
	var badCtx []string
	r := &fakeRunner{fn: func(ctx context.Context, job string) error {
		if ctx.Value(ctxKey{}) != "start-ctx" {
			badCtx = append(badCtx, job)
		}
		return nil
	}}
	s := mustNew(t, r, []Entry{{Name: "ingest", Cron: "0 */6 * * *", Jobs: []string{"ingest", "timeline", "usage", "upstream"}}}, logger)

	s.tick(ctx, s.entries[0])

	if got, want := r.gotCalls(), []string{"ingest", "timeline", "usage", "upstream"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Runner calls = %v, want %v", got, want)
	}
	for i, scope := range r.scopes {
		if scope != (model.Scope{}) {
			t.Errorf("call %d scope = %+v, want zero Scope", i, scope)
		}
	}
	if len(badCtx) != 0 {
		t.Errorf("jobs %v did not get the context given to the chain", badCtx)
	}
	if got := sink.all(); len(got) != 0 {
		t.Errorf("logged %v, want nothing", got)
	}
}

func TestChain_NoJobsRunsNothing(t *testing.T) {
	t.Parallel()
	_, logger := newSink()
	r := &fakeRunner{}
	s := mustNew(t, r, []Entry{{Name: "idle", Cron: "0 3 * * *"}}, logger)
	s.tick(t.Context(), s.entries[0])
	if got := r.gotCalls(); len(got) != 0 {
		t.Errorf("Runner calls = %v, want none", got)
	}
}

func TestChain_ContinuesAfterError(t *testing.T) {
	t.Parallel()
	sink, logger := newSink()
	r := &fakeRunner{fn: func(_ context.Context, job string) error {
		if job == "timeline" {
			return errors.New("run job timeline: boom")
		}
		return nil
	}}
	s := mustNew(t, r, []Entry{{Name: "ingest", Cron: "0 */6 * * *", Jobs: []string{"ingest", "timeline", "usage"}}}, logger)

	s.tick(t.Context(), s.entries[0])

	if got, want := r.gotCalls(), []string{"ingest", "timeline", "usage"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Runner calls = %v, want %v", got, want)
	}
	warns := sink.withMsg("scheduled run returned error")
	if len(warns) != 1 {
		t.Fatalf("got %d %q lines, want 1", len(warns), "scheduled run returned error")
	}
	want := map[string]string{"schedule": "ingest", "job": "timeline", "error": "run job timeline: boom"}
	if warns[0].level != slog.LevelWarn {
		t.Errorf("level = %v, want WARN", warns[0].level)
	}
	if !reflect.DeepEqual(warns[0].attrs, want) {
		t.Errorf("attrs = %v, want %v", warns[0].attrs, want)
	}
	if got := len(sink.all()); got != 1 {
		t.Errorf("logged %d lines, want 1", got)
	}
}

func TestChain_StopsOnCancelledContext(t *testing.T) {
	t.Parallel()
	t.Run("cancelled before the chain", func(t *testing.T) {
		t.Parallel()
		_, logger := newSink()
		r := &fakeRunner{}
		s := mustNew(t, r, []Entry{{Name: "ingest", Cron: "0 */6 * * *", Jobs: []string{"a", "b"}}}, logger)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		s.tick(ctx, s.entries[0])
		if got := r.gotCalls(); len(got) != 0 {
			t.Errorf("Runner calls = %v, want none", got)
		}
	})
	t.Run("cancelled during a job", func(t *testing.T) {
		t.Parallel()
		_, logger := newSink()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		r := &fakeRunner{fn: func(_ context.Context, job string) error {
			if job == "b" {
				cancel()
				return context.Canceled
			}
			return nil
		}}
		s := mustNew(t, r, []Entry{{Name: "ingest", Cron: "0 */6 * * *", Jobs: []string{"a", "b", "c", "d"}}}, logger)
		s.tick(ctx, s.entries[0])
		if got, want := r.gotCalls(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Runner calls = %v, want %v", got, want)
		}
	})
}

func TestTick_SkippedWhilePreviousRunning(t *testing.T) {
	t.Parallel()
	sink, logger := newSink()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var blockOnce sync.Once
	r := &fakeRunner{fn: func(_ context.Context, job string) error {
		if job == "a" {
			blockOnce.Do(func() {
				started <- struct{}{}
				<-release
			})
		}
		return nil
	}}
	s := mustNew(t, r, []Entry{{Name: "issues", Cron: "0 * * * *", Jobs: []string{"a", "b"}}}, logger)
	e := s.entries[0]

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.tick(t.Context(), e)
	}()
	<-started

	s.tick(t.Context(), e) // previous chain still blocked in job a

	if got, want := r.gotCalls(), []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Runner calls while blocked = %v, want %v", got, want)
	}
	skips := sink.withMsg("schedule tick skipped, previous run still in progress")
	if len(skips) != 1 {
		t.Fatalf("got %d skip lines, want 1", len(skips))
	}
	if skips[0].level != slog.LevelWarn || !reflect.DeepEqual(skips[0].attrs, map[string]string{"schedule": "issues"}) {
		t.Errorf("skip line = %+v, want WARN with schedule=issues", skips[0])
	}

	close(release)
	<-done

	// Once the chain has returned, the next tick runs again.
	s.tick(t.Context(), e)
	if got, want := r.gotCalls(), []string{"a", "b", "a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Runner calls = %v, want %v", got, want)
	}
	if got := len(sink.withMsg("schedule tick skipped, previous run still in progress")); got != 1 {
		t.Errorf("got %d skip lines after release, want 1", got)
	}
}

func TestStop_WaitsForRunningChain(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, logger := newSink()
		started := make(chan string, 4)
		release := make(chan struct{})
		r := &fakeRunner{fn: func(_ context.Context, job string) error {
			started <- job
			if job == "a" {
				<-release
			}
			return nil
		}}
		s := mustNew(t, r, []Entry{{Name: "ingest", Cron: "* * * * *", Jobs: []string{"a", "b"}}}, logger)
		s.Start(t.Context())

		// The fake clock reaches the next minute as soon as everything blocks.
		if got := <-started; got != "a" {
			t.Fatalf("first job = %q, want a", got)
		}

		stopErr := make(chan error, 1)
		go func() { stopErr <- s.Stop(t.Context()) }()
		synctest.Wait()
		select {
		case err := <-stopErr:
			t.Fatalf("Stop() returned %v while a chain was running", err)
		default:
		}

		close(release)
		if err := <-stopErr; err != nil {
			t.Fatalf("Stop() error = %v, want nil", err)
		}
		// Stop was called before job b, so b never starts.
		if got, want := r.gotCalls(), []string{"a"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Runner calls = %v, want %v", got, want)
		}
	})
}

func TestStop_ReturnsOnContextDone(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, logger := newSink()
		started := make(chan string, 4)
		release := make(chan struct{})
		r := &fakeRunner{fn: func(_ context.Context, job string) error {
			started <- job
			if job == "a" {
				<-release
			}
			return nil
		}}
		s := mustNew(t, r, []Entry{{Name: "ingest", Cron: "* * * * *", Jobs: []string{"a", "b"}}}, logger)
		s.Start(t.Context())
		<-started

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		err := s.Stop(ctx)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Stop() error = %v, want it to wrap context.DeadlineExceeded", err)
		}

		close(release)
		synctest.Wait()
		if got, want := r.gotCalls(), []string{"a"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Runner calls = %v, want %v (no job may start after Stop)", got, want)
		}
	})
}

func TestStop_CancelledContextWhenIdle(t *testing.T) {
	t.Parallel()
	_, logger := newSink()
	r := &fakeRunner{}
	s := mustNew(t, r, []Entry{{Name: "digest", Cron: "0 8 * * *", Jobs: []string{"digest"}}}, logger)
	s.Start(t.Context())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Stop(ctx); err != nil {
		t.Errorf("Stop(cancelled ctx) with no chain running = %v, want nil", err)
	}
}

func TestTick_AfterStopStartsNoChain(t *testing.T) {
	t.Parallel()
	sink, logger := newSink()
	r := &fakeRunner{}
	s := mustNew(t, r, []Entry{{Name: "issues", Cron: "0 * * * *", Jobs: []string{"issues"}}}, logger)
	s.Start(t.Context())
	if err := s.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	s.tick(t.Context(), s.entries[0]) // a tick cron fired just before it stopped

	if got := r.gotCalls(); len(got) != 0 {
		t.Errorf("Runner calls after Stop = %v, want none", got)
	}
	if got := len(sink.withMsg("schedule tick skipped, previous run still in progress")); got != 0 {
		t.Errorf("got %d skip lines, want 0", got)
	}
}

func TestStop_BeforeStartAndTwice(t *testing.T) {
	t.Parallel()
	entries := []Entry{{Name: "digest", Cron: "0 8 * * *", Jobs: []string{"digest"}}}

	t.Run("before start", func(t *testing.T) {
		t.Parallel()
		sink, logger := newSink()
		s := mustNew(t, &fakeRunner{}, entries, logger)
		if err := s.Stop(t.Context()); err != nil {
			t.Errorf("Stop() before Start error = %v, want nil", err)
		}
		s.Start(t.Context()) // after Stop: no-op
		if got := len(sink.withMsg("schedule registered")); got != 0 {
			t.Errorf("Start() after Stop logged %d registrations, want 0", got)
		}
	})
	t.Run("twice", func(t *testing.T) {
		t.Parallel()
		_, logger := newSink()
		s := mustNew(t, &fakeRunner{}, entries, logger)
		s.Start(t.Context())
		if err := s.Stop(t.Context()); err != nil {
			t.Errorf("first Stop() error = %v, want nil", err)
		}
		if err := s.Stop(t.Context()); err != nil {
			t.Errorf("second Stop() error = %v, want nil", err)
		}
	})
}

func TestChain_PanicRecovered(t *testing.T) {
	t.Parallel()
	t.Run("direct", func(t *testing.T) {
		t.Parallel()
		sink, logger := newSink()
		r := &fakeRunner{fn: func(_ context.Context, job string) error {
			if job == "a" {
				panic("boom")
			}
			return nil
		}}
		s := mustNew(t, r, []Entry{{Name: "ingest", Cron: "0 */6 * * *", Jobs: []string{"a", "b"}}}, logger)

		s.tick(t.Context(), s.entries[0])
		s.tick(t.Context(), s.entries[0]) // not skipped: the panic released the entry

		if got, want := r.gotCalls(), []string{"a", "a"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Runner calls = %v, want %v", got, want)
		}
		panics := sink.withMsg("schedule chain panicked")
		if len(panics) != 2 {
			t.Fatalf("got %d panic lines, want 2", len(panics))
		}
		want := map[string]string{"schedule": "ingest", "panic": "boom"}
		if panics[0].level != slog.LevelError || !reflect.DeepEqual(panics[0].attrs, want) {
			t.Errorf("panic line = %+v, want ERROR with %v", panics[0], want)
		}
		if got := len(sink.withMsg("schedule tick skipped, previous run still in progress")); got != 0 {
			t.Errorf("got %d skip lines, want 0", got)
		}
	})
	t.Run("scheduler keeps ticking", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			sink, logger := newSink()
			started := make(chan struct{}, 8)
			r := &fakeRunner{fn: func(context.Context, string) error {
				select {
				case started <- struct{}{}:
				default:
				}
				panic(errors.New("faulty runner"))
			}}
			s := mustNew(t, r, []Entry{{Name: "issues", Cron: "* * * * *", Jobs: []string{"issues"}}}, logger)
			s.Start(t.Context())
			<-started
			<-started
			if err := s.Stop(t.Context()); err != nil {
				t.Fatalf("Stop() error = %v", err)
			}
			if got := len(sink.withMsg("schedule chain panicked")); got < 2 {
				t.Errorf("got %d panic lines, want at least 2", got)
			}
		})
	})
}

func TestScheduler_FiresOnRealTicks(t *testing.T) {
	t.Parallel()
	sink, logger := newSink()
	seen := make(chan string, 16)
	r := &fakeRunner{fn: func(_ context.Context, job string) error {
		select {
		case seen <- job:
		default:
		}
		return nil
	}}
	s, err := newScheduler(r, []Entry{{Name: "fast", Cron: "20ms", Jobs: []string{"a", "b"}}}, logger, parseDuration)
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}
	s.Start(t.Context())

	timeout := time.After(5 * time.Second)
	var got []string
	for len(got) < 2 {
		select {
		case job := <-seen:
			got = append(got, job)
		case <-timeout:
			t.Fatalf("no tick within 5s; jobs seen %v", got)
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("first chain ran %v, want %v", got, want)
	}
	if n := len(sink.withMsg("schedule registered")); n != 1 {
		t.Errorf("got %d registration lines, want 1", n)
	}
}
