// Command upw mines dependency history, watches advisories and upstream issues,
// and sends a daily digest.
package main

import (
	"context"
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

upw has no commands yet; they are added milestone by milestone.

Run "upw -h" to print this help.
`

// run executes one command and returns the process exit code. ctx and getenv
// are passed to the subcommands, which are added from the next work orders on.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "upw: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
