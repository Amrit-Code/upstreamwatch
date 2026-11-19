package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
)

// jobLockRetry is how often a job waiting for its lock file tries again.
const jobLockRetry = 200 * time.Millisecond

// lockedJob runs a job while holding an exclusive flock on <dir>/<name>.lock.
// The Runner serialises jobs within one process; the lock file does the same
// across processes sharing a data directory, such as "upw run" beside
// "upw serve".
type lockedJob struct {
	job   pipeline.Job
	dir   string
	retry time.Duration
}

// withJobLocks wraps each job so that it runs under its lock file in dir.
func withJobLocks(dir string, jobs []pipeline.Job) []pipeline.Job {
	locked := make([]pipeline.Job, 0, len(jobs))
	for _, j := range jobs {
		locked = append(locked, &lockedJob{job: j, dir: dir, retry: jobLockRetry})
	}
	return locked
}

// Name returns the wrapped job's name.
func (j *lockedJob) Name() string { return j.job.Name() }

// Run waits for the job's lock file, runs the job and releases the lock. If
// ctx ends while it waits, it returns an error wrapping ctx.Err() without
// running the job.
func (j *lockedJob) Run(ctx context.Context, scope model.Scope) (stats model.Stats, err error) {
	release, err := acquireJobLock(ctx, j.dir, j.job.Name(), j.retry)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, release()) }()
	return j.job.Run(ctx, scope)
}

// acquireJobLock takes an exclusive flock on <dir>/<name>.lock, creating the
// directory (mode 0700) and the file (mode 0600) when missing. A lock held
// elsewhere is retried every retry until ctx ends. The returned function
// releases the lock by closing the file.
func acquireJobLock(ctx context.Context, dir, name string, retry time.Duration) (func() error, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create job lock directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, name+".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open job lock %s: %w", name, err)
	}
	release := func() error {
		if err := f.Close(); err != nil {
			return fmt.Errorf("release job lock %s: %w", name, err)
		}
		return nil
	}

	for {
		err := tryLock(f)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return nil, errors.Join(fmt.Errorf("lock job %s: %w", name, err), release())
		}
		// A blocking flock could not be interrupted by ctx, so the wait polls
		// a non-blocking one instead.
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(fmt.Errorf("wait for job lock %s: %w", name, ctx.Err()), release())
		case <-timer.C:
		}
	}
}

// tryLock takes an exclusive flock on f without blocking. It fails with
// EWOULDBLOCK when another open file holds the lock.
func tryLock(f *os.File) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var lockErr error
	if err := rc.Control(func(fd uintptr) {
		lockErr = syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB)
	}); err != nil {
		return err
	}
	return lockErr
}
