package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
)

// overlapRecorder counts the jobs inside Run at once and keeps the maximum.
type overlapRecorder struct {
	mu        sync.Mutex
	active    int
	maxActive int
	runs      int
}

func (o *overlapRecorder) enter() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.active++
	o.runs++
	o.maxActive = max(o.maxActive, o.active)
}

func (o *overlapRecorder) leave() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.active--
}

func TestJobLock_NoOverlap(t *testing.T) {
	t.Parallel()

	t.Run("waiter does not start while the lock is held", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "locks")
			entered := make(chan int, 2)
			release := make(chan struct{})
			newJob := func(id int) *fakeJob {
				return &fakeJob{name: "ingest", run: func(context.Context, model.Scope) (model.Stats, error) {
					entered <- id
					<-release
					return nil, nil
				}}
			}
			// Two Runners, as in two processes: each serialises only its own
			// runs, so only the lock file keeps them apart.
			r1 := newFakeRunner(t, withJobLocks(dir, []pipeline.Job{newJob(1)})...)
			r2 := newFakeRunner(t, withJobLocks(dir, []pipeline.Job{newJob(2)})...)

			errs := make(chan error, 2)
			go func() { _, err := r1.Run(t.Context(), "ingest", model.Scope{}); errs <- err }()
			if got := <-entered; got != 1 {
				t.Fatalf("first job to enter = %d, want 1", got)
			}
			go func() { _, err := r2.Run(t.Context(), "ingest", model.Scope{}); errs <- err }()

			// Let the second runner retry for a while in fake time.
			time.Sleep(5 * jobLockRetry)
			synctest.Wait()
			select {
			case id := <-entered:
				t.Errorf("job %d entered while job 1 held the lock", id)
				// Let both jobs finish so the bubble can end.
				close(release)
				<-errs
				<-errs
				return
			default:
			}

			release <- struct{}{}
			if got := <-entered; got != 2 {
				t.Fatalf("second job to enter = %d, want 2", got)
			}
			release <- struct{}{}
			for range 2 {
				if err := <-errs; err != nil {
					t.Errorf("Run() error = %v, want nil", err)
				}
			}
		})
	})

	t.Run("concurrent runners never overlap", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "locks")
			var rec overlapRecorder
			const runners, runsEach = 3, 3
			var wg sync.WaitGroup
			for range runners {
				job := &fakeJob{name: "timeline", run: func(context.Context, model.Scope) (model.Stats, error) {
					rec.enter()
					defer rec.leave()
					// Simulated work, in fake time.
					time.Sleep(50 * time.Millisecond)
					return nil, nil
				}}
				r := newFakeRunner(t, withJobLocks(dir, []pipeline.Job{job})...)
				for range runsEach {
					wg.Go(func() {
						if _, err := r.Run(t.Context(), "timeline", model.Scope{}); err != nil {
							t.Errorf("Run() error = %v, want nil", err)
						}
					})
				}
			}
			wg.Wait()
			if rec.runs != runners*runsEach {
				t.Errorf("job runs = %d, want %d", rec.runs, runners*runsEach)
			}
			if rec.maxActive != 1 {
				t.Errorf("most jobs running at once = %d, want 1", rec.maxActive)
			}
		})
	})

	t.Run("different jobs do not wait for each other", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "locks")
			release, err := acquireJobLock(t.Context(), dir, "ingest", jobLockRetry)
			if err != nil {
				t.Fatalf("acquireJobLock(ingest) error = %v", err)
			}
			defer func() {
				if err := release(); err != nil {
					t.Errorf("release ingest lock: %v", err)
				}
			}()
			job := &fakeJob{name: "timeline"}
			r := newFakeRunner(t, withJobLocks(dir, []pipeline.Job{job})...)
			start := time.Now()
			if _, err := r.Run(t.Context(), "timeline", model.Scope{}); err != nil {
				t.Fatalf("Run(timeline) error = %v, want nil", err)
			}
			if waited := time.Since(start); waited != 0 {
				t.Errorf("timeline waited %v behind the ingest lock, want no wait", waited)
			}
		})
	})
}

func TestJobLock_CancelledWait(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ctx     func(parent context.Context) (context.Context, context.CancelFunc)
		cancel  bool // cancel once the waiter is polling
		wantErr error
	}{
		{name: "cancelled", ctx: context.WithCancel, cancel: true, wantErr: context.Canceled},
		{name: "deadline", ctx: func(p context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(p, 3*time.Second)
		}, wantErr: context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "locks")
				release, err := acquireJobLock(t.Context(), dir, "timeline", jobLockRetry)
				if err != nil {
					t.Fatalf("acquireJobLock() error = %v", err)
				}
				job := &fakeJob{name: "timeline"}
				locked := withJobLocks(dir, []pipeline.Job{job})[0]

				ctx, cancel := tt.ctx(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() {
					_, err := locked.Run(ctx, model.Scope{})
					done <- err
				}()
				synctest.Wait()
				if tt.cancel {
					time.Sleep(time.Second)
					cancel()
				}
				err = <-done
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Run() error = %v, want %v", err, tt.wantErr)
				}
				if got := job.runScopes(); len(got) != 0 {
					t.Errorf("job ran %d times while its lock was held elsewhere, want 0", len(got))
				}

				if err := release(); err != nil {
					t.Fatalf("release: %v", err)
				}
				if _, err := locked.Run(t.Context(), model.Scope{}); err != nil {
					t.Errorf("Run() after release error = %v, want nil", err)
				}
				if got := len(job.runScopes()); got != 1 {
					t.Errorf("job runs after release = %d, want 1", got)
				}
			})
		})
	}
}

func TestJobLock_ReleasedAfterFailure(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "locks")
	errJob := errors.New("job failed")
	failing := withJobLocks(dir, []pipeline.Job{&fakeJob{name: "ingest", run: func(context.Context, model.Scope) (model.Stats, error) {
		return nil, errJob
	}}})[0]
	if _, err := failing.Run(t.Context(), model.Scope{}); !errors.Is(err, errJob) {
		t.Fatalf("Run() error = %v, want %v", err, errJob)
	}
	// A context that is already done fails a wait at once, so this passes
	// only if the failed run released the lock.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	release, err := acquireJobLock(ctx, dir, "ingest", jobLockRetry)
	if err != nil {
		t.Fatalf("acquireJobLock() after a failed run error = %v, want nil", err)
	}
	if err := release(); err != nil {
		t.Errorf("release: %v", err)
	}
}

func TestJobLock_FileModes(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "data", "locks")
	release, err := acquireJobLock(t.Context(), dir, "ingest", jobLockRetry)
	if err != nil {
		t.Fatalf("acquireJobLock() error = %v", err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Errorf("release: %v", err)
		}
	}()
	for _, tt := range []struct {
		path string
		want os.FileMode
	}{
		{path: dir, want: 0o700},
		{path: filepath.Join(dir, "ingest.lock"), want: 0o600},
	} {
		info, err := os.Stat(tt.path)
		if err != nil {
			t.Fatalf("stat %s: %v", tt.path, err)
		}
		if got := info.Mode().Perm(); got != tt.want {
			t.Errorf("mode of %s = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestJobLock_KeepsName(t *testing.T) {
	t.Parallel()
	jobs := withJobLocks(t.TempDir(), []pipeline.Job{&fakeJob{name: "ingest"}, &fakeJob{name: "timeline"}})
	for i, want := range []string{"ingest", "timeline"} {
		if got := jobs[i].Name(); got != want {
			t.Errorf("jobs[%d].Name() = %q, want %q", i, got, want)
		}
	}
}
