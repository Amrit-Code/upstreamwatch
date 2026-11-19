// Command upw mines dependency history, watches advisories and upstream issues,
// and sends a daily digest.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
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
  run       run one job, or every job once with "run all -once"
  doctor    check the config, secrets, git and the outside services
  help      print this help

Run "upw <command> -h" for a command's flags.
`

// errUsage marks a mistake the user fixes by changing the command line. The
// command has already printed the problem and its usage.
var errUsage = errors.New("usage error")

// errReported marks a failure the command has already reported in its own
// output; run exits 1 without printing it again.
var errReported = errors.New("failure already reported")

// run executes one command and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	var err error
	switch args[0] {
	case "serve":
		err = cmdServe(ctx, args[1:], stdout, stderr, getenv)
	case "run":
		err = cmdRun(ctx, args[1:], stdout, stderr, getenv)
	case "doctor":
		err = cmdDoctor(ctx, args[1:], stdout, stderr, getenv, doctorChecks)
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
		return 2
	case errors.Is(err, errReported):
		return 1
	default:
		fmt.Fprintf(stderr, "upw: %v\n", err)
		return 1
	}
}

// command is one subcommand's flag set with the flags every subcommand
// registers: -config and -log-level.
type command struct {
	fs         *flag.FlagSet
	synopsis   string
	stdout     io.Writer
	stderr     io.Writer
	configPath string
	level      slog.Level
}

// newCommand returns a subcommand whose usage starts with synopsis. Flag
// errors go to stderr; -h prints the usage to stdout.
func newCommand(name, synopsis string, stdout, stderr io.Writer) *command {
	c := &command{synopsis: synopsis, stdout: stdout, stderr: stderr, level: slog.LevelInfo}
	c.fs = flag.NewFlagSet(name, flag.ContinueOnError)
	c.fs.SetOutput(stderr)
	// parse prints the usage itself, to stdout for -h and to stderr otherwise.
	c.fs.Usage = func() {}
	c.fs.StringVar(&c.configPath, "config", "upw.yaml", "`path` to the config file")
	c.fs.Func("log-level", "log `level`: debug, info, warn or error (default info)", c.setLevel)
	return c
}

// setLevel parses the -log-level value.
func (c *command) setLevel(s string) error {
	switch s {
	case "debug":
		c.level = slog.LevelDebug
	case "info":
		c.level = slog.LevelInfo
	case "warn":
		c.level = slog.LevelWarn
	case "error":
		c.level = slog.LevelError
	default:
		return errors.New("want debug, info, warn or error")
	}
	return nil
}

// usage prints the synopsis and the flag defaults to w.
func (c *command) usage(w io.Writer) {
	fmt.Fprint(w, c.synopsis)
	fmt.Fprintln(w, "\nflags:")
	c.fs.SetOutput(w)
	c.fs.PrintDefaults()
	c.fs.SetOutput(c.stderr)
}

// usageError prints a problem with the command line and the usage to stderr
// and returns errUsage.
func (c *command) usageError(format string, args ...any) error {
	fmt.Fprintf(c.stderr, "upw: "+format+"\n\n", args...)
	c.usage(c.stderr)
	return errUsage
}

// parse parses args, which must hold flags only. It returns flag.ErrHelp
// after printing the usage for -h, and errUsage after printing the problem
// and the usage for anything else wrong.
func (c *command) parse(args []string) error {
	err := c.fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		c.usage(c.stdout)
		return flag.ErrHelp
	case err != nil:
		// The flag package has already printed the problem to stderr.
		fmt.Fprintln(c.stderr)
		c.usage(c.stderr)
		return fmt.Errorf("%w: %w", errUsage, err)
	case c.fs.NArg() > 0:
		return c.usageError("unexpected argument %q", c.fs.Arg(0))
	}
	return nil
}

// logger returns the text logger on stderr at the chosen level.
func (c *command) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(c.stderr, &slog.HandlerOptions{Level: c.level}))
}
