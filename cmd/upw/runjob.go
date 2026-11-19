package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
)

const runSynopsis = `usage: upw run <job> [-repo slug] [-config path] [-log-level level]
       upw run timeline [-repo slug] -rebuild [-config path] [-log-level level]
       upw run all -once [-config path] [-log-level level]

Run one job, or every registered job once in pipeline order.
"run timeline -rebuild" discards the stored timeline of every target, or of
the one -repo names, and rebuilds it from the first commit.
Continuous running on the schedules is "upw serve".
`

// runRequest is a parsed "upw run" command line: one job and its scope, or
// job "all" for every registered job once.
type runRequest struct {
	job   string
	scope model.Scope
}

// cmdRun runs one job through the Runner, or every registered job once for
// "run all -once", and prints one line per job on stdout.
func cmdRun(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) (err error) {
	c := newCommand("run", runSynopsis, stdout, stderr)
	req, err := parseRun(c, args)
	if err != nil {
		return err
	}

	a, err := start(ctx, c.configPath, c.logger(), getenv)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.close()) }()
	return runRequested(ctx, a.runner, req, stdout, stderr)
}

// parseRun parses the run command line into c's flags and a runRequest. It
// returns flag.ErrHelp after printing the usage for -h, and errUsage after
// printing the problem and the usage for anything else wrong.
func parseRun(c *command, args []string) (runRequest, error) {
	repo := c.fs.String("repo", "", "limit the job to the target with this `slug`")
	once := c.fs.Bool("once", false, `with "all": run every registered job once, then exit`)
	rebuild := c.fs.Bool("rebuild", false, `with "timeline": discard the stored timeline and rebuild it from the first commit`)

	// flag stops at the first non-flag argument, so the job name comes off
	// the front before the flags are parsed.
	if len(args) > 0 && (args[0] == "-h" || args[0] == "-help" || args[0] == "--help") {
		c.usage(c.stdout)
		return runRequest{}, flag.ErrHelp
	}
	if len(args) == 0 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return runRequest{}, c.usageError("missing job name")
	}
	job := args[0]
	if err := c.parse(args[1:]); err != nil {
		return runRequest{}, err
	}
	switch {
	case job == "all" && !*once:
		return runRequest{}, c.usageError(`"run all" needs -once; continuous running is "upw serve"`)
	case job == "all" && *repo != "":
		return runRequest{}, c.usageError(`-repo cannot be used with "run all"`)
	case job != "all" && *once:
		return runRequest{}, c.usageError(`-once is only used with "run all"`)
	case job != "timeline" && *rebuild:
		return runRequest{}, c.usageError(`-rebuild is only used with "run timeline"`)
	}
	return runRequest{job: job, scope: model.Scope{RepoSlug: *repo, Rebuild: *rebuild}}, nil
}

// runRequested runs req on runner and prints the outcome: one line per job
// for "all", the stats line of a single job that succeeded. A single job's
// failure is returned for main to print.
func runRequested(ctx context.Context, runner *pipeline.Runner, req runRequest, stdout, stderr io.Writer) error {
	if req.job == "all" {
		return runAll(ctx, runner, stdout, stderr)
	}

	run, err := runner.Run(ctx, req.job, req.scope)
	switch {
	case errors.Is(err, pipeline.ErrUnknownJob):
		return fmt.Errorf("unknown job %q; known jobs: %s", req.job, knownJobs(runner.Jobs()))
	case err != nil:
		return err
	}
	fmt.Fprintln(stdout, terminalSafeText(succeededLine(req.job, run.Stats)))
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
			fmt.Fprintf(stdout, "%s: failed: %s\n", job, terminalSafeText(err.Error()))
			continue
		}
		fmt.Fprintln(stdout, terminalSafeText(succeededLine(job, run.Stats)))
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
