package gitsrc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// checkTimeout bounds the git --version probe.
	checkTimeout = 10 * time.Second
	// outputLimit is how much of git's stdout and stderr is kept.
	outputLimit = 4 << 10
)

// errNotInstalled reports that no git executable was found on PATH.
var errNotInstalled = errors.New("git is not installed: no git executable on PATH")

// CLI runs the git binary with an explicit argument list and a minimal
// environment.
type CLI struct {
	logger *slog.Logger
}

// New returns a CLI. It does no I/O.
func New(logger *slog.Logger) *CLI {
	return &CLI{logger: logger}
}

// Check runs git --version and reports whether git is installed and runs.
func (g *CLI) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	path, err := exec.LookPath("git")
	if errors.Is(err, exec.ErrNotFound) {
		return errNotInstalled
	}
	if err != nil {
		return fmt.Errorf("find git: %w", err)
	}

	stdout := &cappedBuffer{limit: outputLimit}
	stderr := &cappedBuffer{limit: outputLimit}
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = environ()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("run git --version: %w: %s", err, msg)
		}
		return fmt.Errorf("run git --version: %w", err)
	}

	version := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(version, "git version ") {
		return fmt.Errorf("run git --version: unexpected output %q", version)
	}
	g.logger.Debug("git found", "path", path, "version", strings.TrimPrefix(version, "git version "))
	return nil
}

// environ is the whole environment git runs with. The process environment
// holds every upstreamwatch secret and git needs none of them, so nothing
// else is inherited.
func environ() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
	}
}

// cappedBuffer keeps the first limit bytes written to it and discards the
// rest, so a misbehaving process cannot fill memory.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

// Write keeps what fits under the limit and reports every byte as written,
// so the process is never blocked or failed by the cap.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// String returns the kept bytes.
func (b *cappedBuffer) String() string { return b.buf.String() }
