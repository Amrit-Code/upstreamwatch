package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
)

const runSynopsis = `usage: upw run <job> [-repo slug] [-config path] [-log-level level]
       upw run all -once [-config path] [-log-level level]

Run one job, or every registered job once in pipeline order.
Continuous running on the schedules is "upw serve".
`

// cmdRun runs one job through the Runner, or every registered job once for
// "run all -once", and prints one line per job on stdout.
func cmdRun(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) (err error) {
	c := newCommand("run", runSynopsis, stdout, stderr)
	repo := c.fs.String("repo", "", "limit the job to the target with this `slug`")
	once := c.fs.Bool("once", false, `with "all": run every registered job once, then exit`)

	// flag stops at the first non-flag argument, so the job name comes off
	// the front before the flags are parsed.
	if len(args) > 0 && (args[0] == "-h" || args[0] == "-help" || args[0] == "--help") {
		c.usage(stdout)
		return nil
	}
	if len(args) == 0 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return c.usageError("missing job name")
	}
	job := args[0]
	if err := c.parse(args[1:]); err != nil {
		return err
	}
	switch {
	case job == "all" && !*once:
		return c.usageError(`"run all" needs -once; continuous running is "upw serve"`)
	case job == "all" && *repo != "":
		return c.usageError(`-repo cannot be used with "run all"`)
	case job != "all" && *once:
		return c.usageError(`-once is only used with "run all"`)
	}

	a, err := start(ctx, c.configPath, c.logger(), getenv)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.close()) }()

	if job == "all" {
		return runAll(ctx, a.runner, stdout, stderr)
	}

	run, err := a.runner.Run(ctx, job, model.Scope{RepoSlug: *repo})
	switch {
	case errors.Is(err, pipeline.ErrUnknownJob):
		return fmt.Errorf("unknown job %q; known jobs: %s", job, knownJobs(a.runner.Jobs()))
	case err != nil:
		return err
	}
	fmt.Fprintln(stdout, succeededLine(job, run.Stats))
	return nil
}

// runAll runs every registered job in pipeline order, continuing after a
// failure, and returns errReported if any job failed.
func runAll(ctx context.Context, runner *pipeline.Runner, stdout, stderr io.Writer) error {
	jobs := runner.Jobs()
	if len(jobs) == 0 {
		fmt.Fprintln(stderr, "upw: no jobs registered yet")
		return nil
	}
	failed := false
	for _, job := range jobs {
		run, err := runner.Run(ctx, job, model.Scope{})
		if err != nil {
			failed = true
			fmt.Fprintf(stdout, "%s: failed: %v\n", job, err)
			continue
		}
		fmt.Fprintln(stdout, succeededLine(job, run.Stats))
	}
	if failed {
		return errReported
	}
	return nil
}

// succeededLine formats a successful run: "<job>: succeeded" followed by
// " <name>=<value>" for each stat in name order.
func succeededLine(job string, stats model.Stats) string {
	var b strings.Builder
	b.WriteString(job + ": succeeded")
	for _, name := range slices.Sorted(maps.Keys(stats)) {
		fmt.Fprintf(&b, " %s=%d", name, stats[name])
	}
	return b.String()
}

// knownJobs lists the registered job names for the unknown-job message.
func knownJobs(jobs []string) string {
	if len(jobs) == 0 {
		return "none registered yet"
	}
	return strings.Join(jobs, ", ")
}
