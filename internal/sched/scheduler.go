package sched

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
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

	// mu guards the fields below and each entry's running flag.
	mu      sync.Mutex
	started bool
	// stopped is set by Stop. Once it is set no chain starts, and a chain
	// that is still running starts no further job.
	stopped bool
	// active counts the chains in progress.
	active int
	// idle is created by Stop when chains are still in progress and closed
	// when the last of them returns.
	idle chan struct{}
}

// entry is a validated Entry with its parsed schedule.
type entry struct {
	name     string
	expr     string
	jobs     []string
	schedule cron.Schedule
	// running is true while a chain of this entry is in progress; a tick that
	// finds it set is skipped. Guarded by Scheduler.mu.
	running bool
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
// returned. Once Stop is called no chain starts and a running chain starts no
// further job. If ctx is done while a chain is still running, Stop returns
// ctx.Err() wrapped; when no chain is running it returns nil whatever the
// state of ctx. Stop before Start, and any call after the first, return nil.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started || s.stopped {
		s.stopped = true
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	var idle chan struct{}
	if s.active > 0 {
		idle = make(chan struct{})
		s.idle = idle
	}
	s.mu.Unlock()

	// The context cron returns is not waited on: chains are counted in
	// active, and a tick cron fires from now on returns without starting one.
	s.cron.Stop()
	if idle == nil {
		return nil
	}
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		// select picks at random when both are ready; a chain that has
		// already returned is not still running.
		select {
		case <-idle:
			return nil
		default:
			return fmt.Errorf("stop scheduler: wait for running schedules: %w", ctx.Err())
		}
	}
}

// tick runs e's chain unless Stop has been called or a previous chain of e is
// still in progress.
func (s *Scheduler) tick(ctx context.Context, e *entry) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if e.running {
		s.mu.Unlock()
		s.logger.Warn("schedule tick skipped, previous run still in progress", "schedule", e.name)
		return
	}
	e.running = true
	s.active++
	s.mu.Unlock()

	defer s.leave(e)
	s.runChain(ctx, e)
}

// leave marks e's chain as returned and signals Stop when it was the last
// chain in progress.
func (s *Scheduler) leave(e *entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.running = false
	s.active--
	if s.active == 0 && s.idle != nil {
		close(s.idle)
		s.idle = nil
	}
}

// isStopped reports whether Stop has been called.
func (s *Scheduler) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
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
		if ctx.Err() != nil || s.isStopped() {
			return
		}
		if _, err := s.runner.Run(ctx, job, model.Scope{}); err != nil {
			s.logger.Warn("scheduled run returned error", "schedule", e.name, "job", job, "error", err.Error())
		}
	}
}
