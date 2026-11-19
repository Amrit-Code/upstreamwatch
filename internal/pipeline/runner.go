package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// ErrUnknownJob reports that Run was asked for a job name that is not
// registered with the Runner.
var ErrUnknownJob = errors.New("unknown job")

// ErrDuplicateJob reports that two jobs given to NewRunner share a name.
var ErrDuplicateJob = errors.New("duplicate job name")

const (
	// jobFailureScore is the fixed score of a job_failure finding (plan
	// section 6, "Ranking").
	jobFailureScore = 50
	// maxReasonErrorBytes caps the job error text quoted in a job_failure
	// finding's rank reason.
	maxReasonErrorBytes = 500
	// recordTimeout bounds the store calls that record a run's outcome, which
	// run on a context detached from the caller's cancellation.
	recordTimeout = 10 * time.Second
)

// Runner runs one registered job at a time, records each run through RunStore
// and raises or clears the job's job_failure finding through FindingStore.
type Runner struct {
	jobs     map[string]Job
	names    []string
	runs     RunStore
	findings FindingStore
	logger   *slog.Logger
	now      func() time.Time
	// lock is a one-slot semaphore held for the whole of a run. Blocked
	// senders on a channel are queued in arrival order, and a select lets a
	// waiting caller give up when its context is cancelled, which sync.Mutex
	// offers neither of.
	lock chan struct{}
}

// NewRunner returns a Runner for jobs, which must have distinct, non-empty
// names. A repeated name is an error wrapping ErrDuplicateJob. It does no I/O.
func NewRunner(jobs []Job, runs RunStore, findings FindingStore, logger *slog.Logger) (*Runner, error) {
	byName := make(map[string]Job, len(jobs))
	names := make([]string, 0, len(jobs))
	for i, job := range jobs {
		if job == nil {
			return nil, fmt.Errorf("register job %d: job is nil", i)
		}
		name := job.Name()
		if name == "" {
			return nil, fmt.Errorf("register job %d: empty job name", i)
		}
		if _, ok := byName[name]; ok {
			return nil, fmt.Errorf("register job %s: %w", name, ErrDuplicateJob)
		}
		byName[name] = job
		names = append(names, name)
	}
	return &Runner{
		jobs:     byName,
		names:    names,
		runs:     runs,
		findings: findings,
		logger:   logger,
		now:      time.Now,
		lock:     make(chan struct{}, 1),
	}, nil
}

// Jobs returns the registered job names in registration order.
func (r *Runner) Jobs() []string {
	return slices.Clone(r.names)
}

// JobFailureFingerprint returns the findings fingerprint for a failed job:
// "job_failure|<job>".
func JobFailureFingerprint(job string) string {
	return string(model.KindJobFailure) + "|" + job
}

// Run runs the named job for scope and records the run. It waits for any run
// already in progress on this Runner to end first. It returns the finished
// run and the job's error wrapped as "run job <name>: ...", joined with any
// error from recording the outcome. A job that panics is recorded and
// returned as a failure whose text starts "panic: ". If ctx is cancelled or
// past its deadline when the job returns a nil error, the run is a failure
// with ctx.Err() as the job error; a job's own non-nil error is kept.
func (r *Runner) Run(ctx context.Context, job string, scope model.Scope) (model.Run, error) {
	j, ok := r.jobs[job]
	if !ok {
		return model.Run{}, fmt.Errorf("run job %s: %w", job, ErrUnknownJob)
	}

	if err := r.acquire(ctx); err != nil {
		return model.Run{}, fmt.Errorf("run job %s: wait for running job: %w", job, err)
	}
	defer r.release()

	start := r.now().UTC()
	run, err := r.runs.StartRun(ctx, job, start)
	if err != nil {
		return model.Run{}, fmt.Errorf("run job %s: start run: %w", job, err)
	}

	stats, jobErr := runJob(ctx, j, scope)
	if jobErr == nil {
		// A job that stopped early on cancellation but reported no error
		// still did not finish its work; record the context error.
		jobErr = ctx.Err()
	}
	finished := r.now().UTC()

	run.FinishedAt = finished
	run.Stats = stats
	// The job error can carry a credential (a URL with userinfo, a token
	// echoed by an outside tool), so only its redacted text leaves the Runner.
	var errText string
	if jobErr != nil {
		errText = redact(jobErr.Error())
		run.Status = model.RunFailed
		run.Error = errText
	} else {
		run.Status = model.RunSucceeded
		run.Error = ""
	}

	// The outcome is recorded even when ctx was cancelled during the job, so
	// that a SIGTERM still leaves a finished runs row and a finding.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()

	var errs []error
	if jobErr != nil {
		errs = append(errs, fmt.Errorf("run job %s: %w", job, &redactedError{msg: errText, err: jobErr}))
	}
	if err := r.runs.FinishRun(recordCtx, run); err != nil {
		errs = append(errs, fmt.Errorf("finish run %d of job %s: %w", run.ID, job, err))
	}

	if jobErr != nil {
		f := model.Finding{
			Kind:          model.KindJobFailure,
			Fingerprint:   JobFailureFingerprint(job),
			Score:         jobFailureScore,
			RankReason:    "job " + job + " failed: " + truncateUTF8(errText, maxReasonErrorBytes),
			LastChangedAt: finished,
		}
		if _, err := r.findings.UpsertFinding(recordCtx, f); err != nil {
			errs = append(errs, fmt.Errorf("raise job_failure finding for job %s: %w", job, err))
		}
		r.logger.Error("job failed", "job", job, "run_id", run.ID, "error", errText)
	} else {
		if err := r.findings.ResolveFinding(recordCtx, JobFailureFingerprint(job), finished); err != nil {
			errs = append(errs, fmt.Errorf("resolve job_failure finding for job %s: %w", job, err))
		}
		r.logger.Info("job finished", finishedAttrs(job, run.ID, stats, finished.Sub(start))...)
	}

	return run, errors.Join(errs...)
}

// acquire takes the run lock, or returns ctx.Err() if ctx is cancelled first.
func (r *Runner) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case r.lock <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	// select picks at random when the lock is free and ctx is already done;
	// a cancelled caller must not start a run either way.
	if err := ctx.Err(); err != nil {
		r.release()
		return err
	}
	return nil
}

func (r *Runner) release() { <-r.lock }

// runJob calls j.Run and turns a panic into an error whose text starts
// "panic: ", so a job's panic never escapes the Runner.
func runJob(ctx context.Context, j Job, scope model.Scope) (stats model.Stats, err error) {
	defer func() {
		if p := recover(); p != nil {
			stats = nil
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return j.Run(ctx, scope)
}

// finishedAttrs builds the attributes of the "job finished" log line: job,
// run_id, duration_ms, then each stat in name order.
func finishedAttrs(job string, runID int64, stats model.Stats, d time.Duration) []any {
	attrs := []any{"job", job, "run_id", runID, "duration_ms", d.Milliseconds()}
	for _, name := range slices.Sorted(maps.Keys(stats)) {
		attrs = append(attrs, name, stats[name])
	}
	return attrs
}

// truncateUTF8 returns s cut to at most n bytes without splitting a UTF-8
// encoded rune.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := n
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}
