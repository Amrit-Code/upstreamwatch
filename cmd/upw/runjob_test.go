package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestRun_UnknownJob(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "bogus", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 1; got != want {
		t.Errorf("run(%q) = %d, want %d", args, got, want)
	}
	want := "upw: unknown job \"bogus\"; known jobs: ingest, timeline\n"
	if got := stderr.String(); !strings.HasSuffix(got, want) {
		t.Errorf("run(%q) stderr = %q, want it to end with %q", args, got, want)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("run(%q) stdout = %q, want empty", args, got)
	}
}

func TestRun_MissingJob(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "no arguments", args: []string{"run"}},
		{name: "flag first", args: []string{"run", "-config", "upw.yaml"}},
		{name: "empty name", args: []string{"run", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if got, want := run(t.Context(), tt.args, &stdout, &stderr, noEnv), 2; got != want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, want)
			}
			if errOut := stderr.String(); !strings.Contains(errOut, "missing job name") || !strings.Contains(errOut, "usage: upw run <job>") {
				t.Errorf("run(%q) stderr = %q, want the problem and the run usage", tt.args, errOut)
			}
			if got := stdout.String(); got != "" {
				t.Errorf("run(%q) stdout = %q, want empty", tt.args, got)
			}
		})
	}
}

func TestRun_AllWithoutOnce(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "all", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 2; got != want {
		t.Errorf("run(%q) = %d, want %d", args, got, want)
	}
	if errOut := stderr.String(); !strings.Contains(errOut, "-once") || !strings.Contains(errOut, `"upw serve"`) {
		t.Errorf("run(%q) stderr = %q, want it to name -once and \"upw serve\"", args, errOut)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("run(%q) stdout = %q, want empty", args, got)
	}
}

func TestRun_OnceAndRepoMisuse(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "once with one job", args: []string{"run", "somejob", "-once", "-config", cfg}, want: `-once is only used with "run all"`},
		{name: "repo with all", args: []string{"run", "all", "-once", "-repo", "x", "-config", cfg}, want: `-repo cannot be used with "run all"`},
		{name: "extra argument", args: []string{"run", "somejob", "extra"}, want: `unexpected argument "extra"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if got, want := run(t.Context(), tt.args, &stdout, &stderr, noEnv), 2; got != want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, want)
			}
			if errOut := stderr.String(); !strings.Contains(errOut, tt.want) {
				t.Errorf("run(%q) stderr = %q, want it to contain %q", tt.args, errOut, tt.want)
			}
		})
	}
}

func TestRun_AllOnceNoTargets(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "all", "-once", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 0; got != want {
		t.Errorf("run(%q) = %d, want %d; stderr %q", args, got, want, stderr.String())
	}
	want := "ingest: succeeded cloned=0 failed=0 fetched=0 targets=0\n" +
		"timeline: succeeded commits_walked=0 events_written=0 failed=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=0\n"
	if got := stdout.String(); got != want {
		t.Errorf("run(%q) stdout = %q, want %q", args, got, want)
	}
}

func TestRun_FlagsAfterJobName(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	var stdout, stderr bytes.Buffer
	// With either flag unparsed this would be a usage error (2) or a missing
	// default config file; reaching the unknown-job error shows both parsed.
	args := []string{"run", "somejob", "-repo", "x", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 1; got != want {
		t.Errorf("run(%q) = %d, want %d; stderr %q", args, got, want, stderr.String())
	}
	if errOut := stderr.String(); !strings.Contains(errOut, `upw: unknown job "somejob"`) {
		t.Errorf("run(%q) stderr = %q, want the unknown-job message", args, errOut)
	}
}

func TestSucceededLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stats model.Stats
		want  string
	}{
		{name: "no stats", stats: nil, want: "ingest: succeeded"},
		{name: "one stat", stats: model.Stats{"fetched": 3}, want: "ingest: succeeded fetched=3"},
		{name: "name order", stats: model.Stats{"zeta": 1, "alpha": 0, "mid": -2}, want: "ingest: succeeded alpha=0 mid=-2 zeta=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := succeededLine("ingest", tt.stats); got != tt.want {
				t.Errorf("succeededLine(ingest, %v) = %q, want %q", tt.stats, got, tt.want)
			}
		})
	}
}

func TestKnownJobs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		jobs []string
		want string
	}{
		{jobs: nil, want: "none registered yet"},
		{jobs: []string{"ingest"}, want: "ingest"},
		{jobs: []string{"ingest", "timeline"}, want: "ingest, timeline"},
	}
	for _, tt := range tests {
		if got := knownJobs(tt.jobs); got != tt.want {
			t.Errorf("knownJobs(%q) = %q, want %q", tt.jobs, got, tt.want)
		}
	}
}
