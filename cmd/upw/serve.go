package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/query"
	"github.com/Amrit-Code/upstreamwatch/internal/sched"
	"github.com/Amrit-Code/upstreamwatch/internal/web"
)

// schedulerStopTimeout bounds the wait for a running scheduled job at shutdown.
const schedulerStopTimeout = 30 * time.Second

const serveSynopsis = `usage: upw serve [-config path] [-log-level level]

Run the scheduler and the web UI until interrupted (SIGINT or SIGTERM).
`

// cmdServe runs the scheduler and the web server until ctx is cancelled, then
// stops them in order: the web server, the scheduler, the store last.
func cmdServe(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) (err error) {
	c := newCommand("serve", serveSynopsis, stdout, stderr)
	if err := c.parse(args); err != nil {
		return err
	}

	a, err := start(ctx, c.configPath, c.logger(), getenv)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.close()) }()

	q := query.NewService(a.db, a.db, a.db, a.logger)
	srv, err := web.NewServer(q, q, web.Options{Listen: a.cfg.HTTP.Listen, BaseURL: a.cfg.HTTP.BaseURL}, a.logger)
	if err != nil {
		return fmt.Errorf("build web server: %w", err)
	}
	scheduler, err := sched.New(a.runner, scheduleEntries(a.cfg.Schedules, a.runner.Jobs()), a.logger)
	if err != nil {
		return fmt.Errorf("build scheduler: %w", err)
	}

	scheduler.Start(ctx)
	// Serve blocks until ctx is cancelled and has shut the HTTP server down
	// by the time it returns.
	serveErr := srv.Serve(ctx)

	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), schedulerStopTimeout)
	defer cancel()
	return errors.Join(serveErr, scheduler.Stop(stopCtx))
}
