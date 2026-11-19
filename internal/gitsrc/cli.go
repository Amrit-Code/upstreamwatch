package gitsrc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	// checkTimeout bounds the git --version probe.
	checkTimeout = 10 * time.Second
	// outputLimit is how much of git's stdout and stderr is kept for errors
	// and small outputs.
	outputLimit = 4 << 10
	// waitDelay bounds how long Wait waits for git's output pipes to close
	// after the process exits or is killed.
	waitDelay = 5 * time.Second
)

// Defaults for the zero values of Options.
const (
	defaultMaxCloneBytes = 2048 << 20
	defaultCloneTimeout  = 30 * time.Minute
	defaultFetchTimeout  = 10 * time.Minute
	defaultMaxBlobBytes  = 8 << 20
	defaultMaxLogBytes   = 64 << 20
)

// errNotInstalled reports that no git executable was found on PATH.
var errNotInstalled = errors.New("git is not installed: no git executable on PATH")

// Options configures a CLI.
type Options struct {
	ReposDir      string        // clones live at ReposDir/<slug>.git; created 0700
	HomeDir       string        // HOME for every git process: an empty directory, created 0700
	MaxCloneBytes int64         // size cap on one clone directory; zero means 2048 MiB
	CloneTimeout  time.Duration // deadline for a first sync; zero means 30 minutes
	FetchTimeout  time.Duration // deadline for a later sync and for ls-remote; zero means 10 minutes
	MaxBlobBytes  int64         // ReadBlobs cap per blob; zero means 8 MiB
	MaxLogBytes   int64         // cap on the git log output ManifestCommits reads; zero means 64 MiB
	// AllowFileURLs lets Sync accept file:// URLs and skips the address
	// check for them. Only tests set it, to sync from fixture repositories.
	AllowFileURLs bool
	// LookupHost resolves a host for the address check; nil means
	// net.DefaultResolver.LookupNetIP with network "ip".
	LookupHost func(ctx context.Context, host string) ([]netip.Addr, error)
}

// CLI runs the git binary with an explicit argument list, a minimal
// environment and per-invocation config that refuses every protocol but
// https, credential helpers, redirects, submodules and hooks.
type CLI struct {
	opts   Options
	logger *slog.Logger

	// dirsOnce guards homeDir and reposDir, HomeDir and ReposDir made
	// absolute on first use.
	dirsOnce sync.Once
	homeDir  string
	reposDir string
	dirsErr  error
}

// New returns a CLI with the zero values of opts replaced by their defaults.
// It does no I/O.
func New(opts Options, logger *slog.Logger) *CLI {
	if opts.MaxCloneBytes <= 0 {
		opts.MaxCloneBytes = defaultMaxCloneBytes
	}
	if opts.CloneTimeout <= 0 {
		opts.CloneTimeout = defaultCloneTimeout
	}
	if opts.FetchTimeout <= 0 {
		opts.FetchTimeout = defaultFetchTimeout
	}
	if opts.MaxBlobBytes <= 0 {
		opts.MaxBlobBytes = defaultMaxBlobBytes
	}
	if opts.MaxLogBytes <= 0 {
		opts.MaxLogBytes = defaultMaxLogBytes
	}
	if opts.LookupHost == nil {
		opts.LookupHost = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	return &CLI{opts: opts, logger: logger}
}

// Check runs git --version and reports whether git is installed and runs.
func (g *CLI) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	out, err := g.output(ctx, invocation{args: []string{"--version"}})
	if err != nil {
		return err
	}
	version := strings.TrimSpace(out)
	if !strings.HasPrefix(version, "git version ") {
		return fmt.Errorf("run git --version: unexpected output %q", version)
	}
	g.logger.Debug("git found", "version", strings.TrimPrefix(version, "git version "))
	return nil
}

// dirs returns HomeDir and ReposDir as absolute paths, empty when unset.
// git runs with HomeDir as its working directory, so a relative path handed
// to it would resolve somewhere else than the same path in this process.
func (g *CLI) dirs() (home, repos string, err error) {
	g.dirsOnce.Do(func() {
		g.homeDir, g.dirsErr = absPath(g.opts.HomeDir)
		if g.dirsErr == nil {
			g.reposDir, g.dirsErr = absPath(g.opts.ReposDir)
		}
	})
	return g.homeDir, g.reposDir, g.dirsErr
}

// absPath returns p as an absolute path, or "" for "".
func absPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("make %s absolute: %w", p, err)
	}
	return abs, nil
}

// invocation is one git process.
type invocation struct {
	// gitDir is passed as --git-dir, so git never searches parent
	// directories for a repository; empty for commands that need none. It
	// must be absolute, because git runs in HomeDir.
	gitDir string
	// config holds extra key=value pairs passed with -c.
	config []string
	// env holds extra environment entries, such as the credential header.
	env []string
	// secrets are credential strings sent with this invocation, scrubbed
	// from any error text it produces.
	secrets []string
	args    []string
	stdout  io.Writer
}

// name is the git subcommand, for error messages. It never includes
// arguments, which may carry a URL.
func (inv invocation) name() string {
	if len(inv.args) == 0 {
		return ""
	}
	return inv.args[0]
}

// command builds the exec.Cmd for inv. stderr collects git's standard error
// up to outputLimit.
func (g *CLI) command(ctx context.Context, inv invocation, stderr *cappedBuffer) (*exec.Cmd, error) {
	path, err := exec.LookPath("git")
	if errors.Is(err, exec.ErrNotFound) {
		return nil, errNotInstalled
	}
	if err != nil {
		return nil, fmt.Errorf("find git: %w", err)
	}
	home, _, err := g.dirs()
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, 2*len(baseConfig)+2*len(inv.config)+4+len(inv.args))
	for _, kv := range baseConfig {
		args = append(args, "-c", kv)
	}
	if g.opts.AllowFileURLs {
		args = append(args, "-c", "protocol.file.allow=always")
	}
	for _, kv := range inv.config {
		args = append(args, "-c", kv)
	}
	if inv.gitDir != "" {
		args = append(args, "--git-dir="+inv.gitDir)
	}
	args = append(args, inv.args...)

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(environ(home), inv.env...)
	cmd.Dir = home
	cmd.Stdout = inv.stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay
	// git runs helpers as child processes (index-pack, the transport and
	// upload-pack). Killing only git on cancellation would leave them
	// writing into the clone while it is pruned or removed, so git leads
	// its own process group and cancellation kills the whole group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	return cmd, nil
}

// killGroup sends SIGKILL to the process group led by pid. A group that has
// already exited is reported as os.ErrProcessDone, which Wait ignores.
func killGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// run runs inv to completion.
func (g *CLI) run(ctx context.Context, inv invocation) error {
	stderr := &cappedBuffer{limit: outputLimit}
	cmd, err := g.command(ctx, inv, stderr)
	if err != nil {
		return err
	}
	return runError(ctx, inv.name(), cmd.Run(), stderr, inv.secrets)
}

// output runs inv and returns its standard output, capped at outputLimit.
func (g *CLI) output(ctx context.Context, inv invocation) (string, error) {
	stdout := &cappedBuffer{limit: outputLimit}
	inv.stdout = stdout
	if err := g.run(ctx, inv); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

// runError turns the result of running git subcommand name into an error
// carrying git's stderr, with secrets and anything else that looks like a
// credential redacted. A remote can echo the credential it was sent, so the
// stderr text is never trusted. The error never includes the environment or
// the arguments.
func runError(ctx context.Context, name string, err error, stderr *cappedBuffer, secrets []string) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = fmt.Errorf("%w (%w)", ctxErr, err)
	}
	if msg := strings.TrimSpace(scrub(stderr.String(), stderr.truncated, secrets)); msg != "" {
		return fmt.Errorf("run git %s: %w: %s", name, err, msg)
	}
	return fmt.Errorf("run git %s: %w", name, err)
}

// redacted replaces each secret scrub removes.
const redacted = "[REDACTED]"

// scrub replaces every occurrence of each secret in s, then passes s through
// model.Redact, then replaces every control character other than newline and
// tab, and every byte that is not valid UTF-8, with "?". When s was cut at
// the output cap, a tail of s that is the start of a secret is replaced too,
// since the cut can split one. A hostile remote writes git's stderr, so
// without the last step it could put terminal escape sequences into error
// text that an operator reads on a terminal.
func scrub(s string, truncated bool, secrets []string) string {
	return replaceControl(redactSecrets(s, truncated, secrets))
}

// redactSecrets is the credential part of scrub.
func redactSecrets(s string, truncated bool, secrets []string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, redacted)
		}
	}
	if truncated {
		// The longest tail that starts any secret is cut, so a short
		// accidental match with one secret cannot hide a longer one.
		cut := 0
		for _, sec := range secrets {
			for n := min(len(sec)-1, len(s)); n > cut; n-- {
				if strings.HasSuffix(s, sec[:n]) {
					cut = n
					break
				}
			}
		}
		if cut > 0 {
			s = s[:len(s)-cut] + redacted
		}
	}
	return model.Redact(s)
}

// replaceControl returns s with every rune for which unicode.IsControl is
// true, other than '\n' and '\t', and every invalid UTF-8 byte replaced by
// '?'.
func replaceControl(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && size <= 1:
			b.WriteByte('?')
		case r != '\n' && r != '\t' && unicode.IsControl(r):
			b.WriteByte('?')
		default:
			b.WriteString(s[:size])
		}
		s = s[size:]
	}
	return b.String()
}

// exitCode returns the process exit code wrapped in err, or -1 when err is
// not an exit status.
func exitCode(err error) int {
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return ee.ExitCode()
	}
	return -1
}

// baseConfig is the per-invocation config every git process gets. None of it
// is secret.
var baseConfig = []string{
	"protocol.allow=never",
	"protocol.https.allow=always",
	"credential.helper=",
	"http.followRedirects=false",
	"fetch.recurseSubmodules=false",
	"core.hooksPath=" + os.DevNull,
	"http.lowSpeedLimit=1000",
	"http.lowSpeedTime=60",
	// No automatic gc or maintenance: git can start it detached after a
	// fetch, in a new session that escapes the process-group kill, and it
	// would then write into a clone that is being pruned or removed.
	"gc.auto=0",
	"gc.autoDetach=false",
	"maintenance.auto=false",
}

// environ is the whole environment git runs with, before any credential
// entries. The process environment holds every upstreamwatch secret and git
// needs none of them, so nothing else is inherited. HOME is set only when
// home is not empty: Check runs without one.
func environ(home string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
	}
	if home != "" {
		env = append(env, "HOME="+home)
	}
	return env
}

// cappedBuffer keeps the first limit bytes written to it and discards the
// rest, so a misbehaving process cannot fill memory.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
	// truncated is set once a write did not fit.
	truncated bool
}

// Write keeps what fits under the limit and reports every byte as written,
// so the process is never blocked or failed by the cap.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	room := b.limit - b.buf.Len()
	if room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	if len(p) > room {
		b.truncated = true
	}
	return len(p), nil
}

// String returns the kept bytes.
func (b *cappedBuffer) String() string { return b.buf.String() }

// formatMiB formats n bytes as a number of MiB for error messages.
func formatMiB(n int64) string {
	return strconv.FormatFloat(float64(n)/(1<<20), 'f', -1, 64)
}
