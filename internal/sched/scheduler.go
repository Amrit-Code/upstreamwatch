package sched

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Entry is one schedule: a cron expression and the jobs it runs, in order.
type Entry struct {
	Name string   // the config schedules key, for example "ingest"
	Cron string   // five-field cron expression, evaluated in the process's local time zone
	Jobs []string // job names handed to the Runner, in order
}

// Scheduler runs each Entry's chain of jobs through the Runner whenever the
// entry's cron expression fires. It holds no job logic and records nothing;
// the Runner does both.
type Scheduler struct {
	runner  Runner
	entries []*entry
	logger  *slog.Logger
	cron    *cron.Cron

	mu      sync.Mutex
	started bool
	stopped bool
	// stopping is set by Stop before it waits, so a chain that is still
	// running starts no further job.
	stopping atomic.Bool
}

// entry is a validated Entry with its parsed schedule.
type entry struct {
	name     string
	expr     string
	jobs     []string
	schedule cron.Schedule
	// running is true while a chain of this entry is in progress; a tick that
	// finds it set is skipped.
	running atomic.Bool
}

// parseFunc turns a cron expression into a schedule. New uses
// parseFiveField; in-package tests swap in faster schedules.
type parseFunc func(expr string) (cron.Schedule, error)

// fiveFieldParser accepts minute, hour, day of month, month and day of week,
// and no seconds field or descriptors.
var fiveFieldParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// parseFiveField parses a standard five-field cron expression evaluated in
// time.Local.
func parseFiveField(expr string) (cron.Schedule, error) {
	// The cron library accepts a TZ= or CRON_TZ= prefix, which would override
	// the local time zone that every schedule is defined in, and it panics on
	// such a prefix with nothing after it.
	if strings.HasPrefix(expr, "TZ=") || strings.HasPrefix(expr, "CRON_TZ=") {
		return nil, errors.New("time zone prefix not allowed, schedules use the local time zone")
	}
	return fiveFieldParser.Parse(expr)
}

// New validates entries and returns a Scheduler that runs them through
// runner once started. It reports every invalid entry at once: an empty or
// duplicate name, a cron expression that is not standard five-field cron, or
// an empty job name. An entry with no jobs is valid and runs nothing. New does
// no I/O and starts nothing.
func New(runner Runner, entries []Entry, logger *slog.Logger) (*Scheduler, error) {
	return newScheduler(runner, entries, logger, parseFiveField)
}

// newScheduler is New with the cron parser as a parameter.
func newScheduler(runner Runner, entries []Entry, logger *slog.Logger, parse parseFunc) (*Scheduler, error) {
	var errs []error
	seen := make(map[string]bool, len(entries))
	parsed := make([]*entry, 0, len(entries))
	for i, e := range entries {
		label := e.Name
		switch {
		case e.Name == "":
			label = fmt.Sprintf("at index %d", i)
			errs = append(errs, fmt.Errorf("schedule %s: empty name", label))
		case seen[e.Name]:
			errs = append(errs, fmt.Errorf("schedule %s: duplicate name", label))
		}
		seen[e.Name] = true

		schedule, err := parse(e.Cron)
		if err != nil {
			errs = append(errs, fmt.Errorf("schedule %s: parse cron expression %q: %w", label, e.Cron, err))
		}
		for j, job := range e.Jobs {
			if job == "" {
				errs = append(errs, fmt.Errorf("schedule %s: job %d: empty job name", label, j))
			}
		}
		parsed = append(parsed, &entry{
			name:     e.Name,
			expr:     e.Cron,
			jobs:     slices.Clone(e.Jobs),
			schedule: schedule,
		})
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &Scheduler{
		runner:  runner,
		entries: parsed,
		logger:  logger,
		cron:    cron.New(cron.WithLocation(time.Local), cron.WithLogger(cron.DiscardLogger)),
	}, nil
}

// Start logs each registered schedule and starts ticking. Every chain runs
// with ctx. It returns immediately. A second call, or a call after Stop, does
// nothing. Stop must be called to end the ticking.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return
	}
	s.started = true

	for _, e := range s.entries {
		s.logger.Info("schedule registered", "schedule", e.name, "cron", e.expr, "jobs", strings.Join(e.jobs, ","))
		s.cron.Schedule(e.schedule, cron.FuncJob(func() { s.tick(ctx, e) }))
	}
	s.cron.Start()
}

// Stop stops new ticks and waits until every chain already running has
// returned. A running chain starts no further job once Stop is called. If ctx
// is done first, Stop returns ctx.Err() wrapped. Stop before Start, and any
// call after the first, return nil.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started || s.stopped {
		s.stopped = true
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.stopping.Store(true)
	s.mu.Unlock()

	done := s.cron.Stop()
	select {
	case <-done.Done():
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop scheduler: wait for running schedules: %w", ctx.Err())
	}
}

// tick runs e's chain unless a previous chain of e is still in progress.
func (s *Scheduler) tick(ctx context.Context, e *entry) {
	if !e.running.CompareAndSwap(false, true) {
		s.logger.Warn("schedule tick skipped, previous run still in progress", "schedule", e.name)
		return
	}
	defer e.running.Store(false)
	s.runChain(ctx, e)
}

// runChain hands e's jobs to the Runner one after another. A job's error is
// logged and the next job still runs; a cancelled ctx or a call to Stop skips
// the rest of the chain. A panic ends the chain and is logged.
func (s *Scheduler) runChain(ctx context.Context, e *entry) {
	defer func() {
		if p := recover(); p != nil {
			s.logger.Error("schedule chain panicked", "schedule", e.name, "panic", fmt.Sprint(p))
		}
	}()
	for _, job := range e.jobs {
		if ctx.Err() != nil || s.stopping.Load() {
			return
		}
		if _, err := s.runner.Run(ctx, job, model.Scope{}); err != nil {
			s.logger.Warn("scheduled run returned error", "schedule", e.name, "job", job, "error", err.Error())
		}
	}
}
