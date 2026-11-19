package main

import (
	"bytes"
	"strings"
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
