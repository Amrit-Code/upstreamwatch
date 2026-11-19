package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/sched"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

// app holds what the shared start-up wires together. runner is nil for
// commands that only read the store.
type app struct {
	cfg    config.Config
	logger *slog.Logger
	db     *store.DB
	runner *pipeline.Runner
}

// openApp loads the config and opens and migrates the store. The errors of
// config and store are returned unwrapped: they already name the field or
// step that failed. On error nothing is left open.
func openApp(ctx context.Context, configPath string, logger *slog.Logger, getenv func(string) string) (*app, error) {
	cfg, err := config.Load(configPath, getenv)
	if err != nil {
		return nil, err
	}

	db, err := store.Open(ctx, filepath.Join(cfg.DataDir, "upw.sqlite"), logger)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		return nil, errors.Join(err, closeStore(db))
	}
	return &app{cfg: cfg, logger: logger, db: db}, nil
}

// start does what openApp does and builds the Runner with every registered
// job. A notify channel that cannot be built fails start-up; the error names
// the channel, never its secret. On error nothing is left open.
func start(ctx context.Context, configPath string, logger *slog.Logger, getenv func(string) string) (*app, error) {
	a, err := openApp(ctx, configPath, logger, getenv)
	if err != nil {
		return nil, err
	}

	jobs, err := registeredJobs(a.cfg, a.db, logger)
	if err != nil {
		return nil, errors.Join(err, a.close())
	}
	// Passing db as RunStore and FindingStore is the compile-time check that
	// *store.DB satisfies the Runner's store interfaces.
	runner, err := pipeline.NewRunner(jobs, a.db, a.db, logger)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("build runner: %w", err), a.close())
	}
	a.runner = runner
	return a, nil
}

// close releases the store.
func (a *app) close() error { return closeStore(a.db) }

func closeStore(db *store.DB) error {
	if err := db.Close(); err != nil {
		return fmt.Errorf("close store: %w", err)
	}
	return nil
}

// scheduleEntries returns one entry per config schedules key, in a fixed
// order, each with its chain of jobs (docs/architecture.md, "Schedules").
// Jobs not registered with the Runner are left out of their chain, so an
// entry may have no jobs; it is still scheduled, and logged at start.
func scheduleEntries(s config.Schedules, registered []string) []sched.Entry {
	chains := []struct {
		name, cron string
		jobs       []string
	}{
		{"ingest", s.Ingest, []string{"ingest", "timeline", "usage", "upstream"}},
		{"vulns", s.Vulns, []string{"vulns"}},
		{"issues", s.Issues, []string{"issues", "prefilter"}},
		{"llm", s.LLM, []string{"triage"}},
		{"digest", s.Digest, []string{"digest"}},
		{"backup", s.Backup, []string{"backup"}},
	}
	entries := make([]sched.Entry, 0, len(chains))
	for _, c := range chains {
		var jobs []string
		for _, job := range c.jobs {
			if slices.Contains(registered, job) {
				jobs = append(jobs, job)
			}
		}
		entries = append(entries, sched.Entry{Name: c.name, Cron: c.cron, Jobs: jobs})
	}
	return entries
}
