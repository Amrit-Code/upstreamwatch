# Scaffold: cmd/upw main, subcommands and serve

Use this when creating or extending `cmd/upw`. The shape is what matters. Constructor names for `sched`, `web` and the services are whatever those packages export; the command list is in `docs/architecture.md` (CLI table).

## main and run

```go
// Command upw mines dependency history, watches advisories and upstream issues,
// and sends a daily digest.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

const usage = `usage: upw <command> [flags]

commands:
  serve     run the scheduler and the web UI until interrupted
  run       run one job, or all jobs once
  timeline  print a repository's dependency timeline
  findings  print open findings
  doctor    check tokens, git and outside services

Run "upw <command> -h" for a command's flags.
`

// errUsage marks a mistake the user fixes by changing the command line.
var errUsage = errors.New("usage error")

// run executes one command and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	var err error
	switch args[0] {
	case "serve":
		err = cmdServe(ctx, args[1:], stderr, getenv)
	case "run":
		err = cmdRun(ctx, args[1:], stdout, stderr, getenv)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "upw: unknown command %q\n\n%s", args[0], usage)
		return 2
	}

	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errUsage):
		return 2 // the flag set or the command has already printed usage
	default:
		fmt.Fprintf(stderr, "upw: %v\n", err)
		return 1
	}
}

// newFlagSet returns a flag set for one subcommand with the shared -config flag.
func newFlagSet(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "upw.yaml", "path to the config `file`")
	return fs, configPath
}

// parseFlags parses args and marks parse failures as usage errors.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	return nil
}
```

## A subcommand with a positional argument

The `flag` package stops at the first non-flag argument. `upw run issues --repo server` therefore has to take `issues` off before parsing, or `--repo` is never seen.

```go
func cmdRun(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	fs, configPath := newFlagSet("run", stderr)
	repo := fs.String("repo", "", "limit the job to one target `slug`")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: upw run <job|all> [-repo slug] [-config file]")
		fs.PrintDefaults()
	}

	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fs.Usage()
		return flag.ErrHelp
	}
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fs.Usage()
		return errUsage
	}
	job := args[0]
	if err := parseFlags(fs, args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return errUsage
	}

	a, err := build(ctx, *configPath, stderr, getenv)
	if err != nil {
		return err
	}
	defer a.Close()

	stats, err := a.runner.Run(ctx, job, model.Scope{Repo: *repo})
	if err != nil {
		return fmt.Errorf("run %s: %w", job, err)
	}
	fmt.Fprintln(stdout, stats) // command output goes to stdout; logs went to stderr
	return nil
}
```

## Wiring

`build` is the one function that imports `config`, `store` and the adapter packages. It works in this order:

1. Load config.
2. Build the logger.
3. Open the store and apply migrations.
4. Construct the adapters.
5. Construct the services with the store and adapters as interface values.
6. Construct the Runner with the jobs.

Every command that touches the database goes through it, so migrations always run at startup.

```go
// app holds what build wires together. Close releases it in reverse order.
type app struct {
	cfg    config.Config
	logger *slog.Logger
	db     *store.DB
	runner *pipeline.Runner
	// sched and web are set for serve.
}

func build(ctx context.Context, configPath string, stderr io.Writer, getenv func(string) string) (*app, error) {
	cfg, err := config.Load(configPath, getenv)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	db, err := store.Open(ctx, filepath.Join(cfg.DataDir, "upw.sqlite"), logger)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate store: %w", err)
	}

	// Adapters, then services, then the Runner, each constructed explicitly here.
	// Passing db where a service expects its own store interface is the compile-time
	// check that *store.DB satisfies it.
	return &app{cfg: cfg, logger: logger, db: db /* runner: ... */}, nil
}

// Close releases the store.
func (a *app) Close() error { return a.db.Close() }
```

## serve: start, block, shut down in order

```go
func cmdServe(ctx context.Context, args []string, stderr io.Writer, getenv func(string) string) error {
	fs, configPath := newFlagSet("serve", stderr)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	a, err := build(ctx, *configPath, stderr, getenv)
	if err != nil {
		return err
	}
	defer a.Close() // the store closes last, after the server and scheduler below

	srv := &http.Server{Addr: a.cfg.HTTP.Listen, Handler: a.web, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }() // returns once Shutdown is called
	a.sched.Start()
	a.logger.Info("upw serving", "listen", a.cfg.HTTP.Listen)

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		runErr = fmt.Errorf("http server: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("http shutdown: %w", err))
	}
	if err := a.sched.Stop(shutdownCtx); err != nil { // waits for the running job to return
		runErr = errors.Join(runErr, fmt.Errorf("scheduler stop: %w", err))
	}
	a.logger.Info("upw stopped")
	return runErr
}
```
