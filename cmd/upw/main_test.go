package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func noEnv(string) string { return "" }

func TestRunNoArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "nil", args: nil},
		{name: "empty", args: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			got := run(t.Context(), tt.args, &stdout, &stderr, noEnv)
			if want := 2; got != want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, want)
			}
			if got, want := stderr.String(), usage; got != want {
				t.Errorf("run(%q) stderr = %q, want %q", tt.args, got, want)
			}
			if got := stdout.String(); got != "" {
				t.Errorf("run(%q) stdout = %q, want empty", tt.args, got)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "short flag", args: []string{"-h"}},
		{name: "long flag", args: []string{"--help"}},
		{name: "help command", args: []string{"help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			got := run(t.Context(), tt.args, &stdout, &stderr, noEnv)
			if want := 0; got != want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, want)
			}
			if got, want := stdout.String(), usage; got != want {
				t.Errorf("run(%q) stdout = %q, want %q", tt.args, got, want)
			}
			if got := stderr.String(); got != "" {
				t.Errorf("run(%q) stderr = %q, want empty", tt.args, got)
			}
		})
	}
}

func TestRunHelp_ListsCommands(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{"serve", "run", "help"} {
		if !strings.Contains(usage, "\n  "+cmd+" ") {
			t.Errorf("usage does not list %q:\n%s", cmd, usage)
		}
	}
}

func TestRunUnknownCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown word", args: []string{"bogus"}, want: `upw: unknown command "bogus"`},
		{name: "unknown flag", args: []string{"-x"}, want: `upw: unknown command "-x"`},
		{name: "extra arguments", args: []string{"bogus", "-config", "upw.yaml"}, want: `upw: unknown command "bogus"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			got := run(t.Context(), tt.args, &stdout, &stderr, noEnv)
			if want := 2; got != want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, want)
			}
			errOut := stderr.String()
			if !strings.Contains(errOut, tt.want) {
				t.Errorf("run(%q) stderr = %q, want it to contain %q", tt.args, errOut, tt.want)
			}
			if !strings.HasSuffix(errOut, usage) {
				t.Errorf("run(%q) stderr = %q, want it to end with the usage", tt.args, errOut)
			}
			if got := stdout.String(); got != "" {
				t.Errorf("run(%q) stdout = %q, want empty", tt.args, got)
			}
		})
	}
}

func TestRun_SubcommandHelp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "serve -h", args: []string{"serve", "-h"}, want: "usage: upw serve"},
		{name: "serve --help", args: []string{"serve", "--help"}, want: "usage: upw serve"},
		{name: "run -h", args: []string{"run", "-h"}, want: "usage: upw run <job>"},
		{name: "run job -h", args: []string{"run", "somejob", "-h"}, want: "usage: upw run <job>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			got := run(t.Context(), tt.args, &stdout, &stderr, noEnv)
			if want := 0; got != want {
				t.Errorf("run(%q) = %d, want %d; stderr %q", tt.args, got, want, stderr.String())
			}
			out := stdout.String()
			if !strings.HasPrefix(out, tt.want) || !strings.Contains(out, "-config path") || !strings.Contains(out, "-log-level level") {
				t.Errorf("run(%q) stdout = %q, want usage starting %q listing -config and -log-level", tt.args, out, tt.want)
			}
			if got := stderr.String(); got != "" {
				t.Errorf("run(%q) stderr = %q, want empty", tt.args, got)
			}
		})
	}
}

func TestServe_UsageErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown flag", args: []string{"serve", "-bogus"}, want: "flag provided but not defined: -bogus"},
		{name: "extra argument", args: []string{"serve", "extra"}, want: `upw: unexpected argument "extra"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			got := run(t.Context(), tt.args, &stdout, &stderr, noEnv)
			if want := 2; got != want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, want)
			}
			errOut := stderr.String()
			if !strings.Contains(errOut, tt.want) || !strings.Contains(errOut, "usage: upw serve") {
				t.Errorf("run(%q) stderr = %q, want %q and the usage", tt.args, errOut, tt.want)
			}
			if got := stdout.String(); got != "" {
				t.Errorf("run(%q) stdout = %q, want empty", tt.args, got)
			}
		})
	}
}

func TestLogLevel_Invalid(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	tests := []struct {
		name string
		args []string
	}{
		{name: "serve", args: []string{"serve", "-log-level", "verbose", "-config", cfg}},
		{name: "run", args: []string{"run", "somejob", "-log-level", "INFO", "-config", cfg}},
		{name: "run all", args: []string{"run", "all", "-once", "-log-level", "", "-config", cfg}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			got := run(t.Context(), tt.args, &stdout, &stderr, noEnv)
			if want := 2; got != want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, want)
			}
			if errOut := stderr.String(); !strings.Contains(errOut, "-log-level") || !strings.Contains(errOut, "want debug, info, warn or error") {
				t.Errorf("run(%q) stderr = %q, want the -log-level problem", tt.args, errOut)
			}
			if got := stdout.String(); got != "" {
				t.Errorf("run(%q) stdout = %q, want empty", tt.args, got)
			}
			if _, err := os.Stat(dbPath(cfg)); err == nil {
				t.Errorf("run(%q) created the database, want nothing opened on a usage error", tt.args)
			}
		})
	}
}

func TestLogLevel_Valid(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			cfg := writeConfig(t, "")
			var stdout, stderr bytes.Buffer
			args := []string{"run", "all", "-once", "-log-level", level, "-config", cfg}
			if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 0; got != want {
				t.Errorf("run(%q) = %d, want %d; stderr %q", args, got, want, stderr.String())
			}
		})
	}
}

// writeConfig writes a config to a fresh temporary directory, with data_dir
// inside it, http.listen on a free loopback port and extra appended, and
// returns its path.
func writeConfig(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	content := "data_dir: " + strconv.Quote(filepath.Join(dir, "data")) + "\n" +
		"http:\n  listen: 127.0.0.1:0\n" + extra
	path := filepath.Join(dir, "upw.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// dbPath returns where serve and run keep the database for a config written
// by writeConfig.
func dbPath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "data", "upw.sqlite")
}

// syncBuffer is a bytes.Buffer safe for concurrent use. When marker is set,
// found is closed the first time the written text contains it.
type syncBuffer struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	marker string
	found  chan struct{}
	once   sync.Once
}

func newSyncBuffer(marker string) *syncBuffer {
	return &syncBuffer{marker: marker, found: make(chan struct{})}
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if b.marker != "" && strings.Contains(b.buf.String(), b.marker) {
		b.once.Do(func() { close(b.found) })
	}
	return n, err
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
