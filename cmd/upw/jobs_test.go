package main

import (
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
)

func TestGitOptions_Strict(t *testing.T) {
	t.Parallel()
	dataDir := filepath.Join("srv", "upw-data")
	cfg := config.Config{
		DataDir: dataDir,
		Git:     config.Git{MaxCloneMB: 512, CloneTimeout: 45 * time.Minute, FetchTimeout: 5 * time.Minute},
	}
	got := gitOptions(cfg)
	want := gitsrc.Options{
		ReposDir:      filepath.Join(dataDir, "repos"),
		HomeDir:       filepath.Join(dataDir, "git-home"),
		MaxCloneBytes: 512 * 1024 * 1024,
		CloneTimeout:  45 * time.Minute,
		FetchTimeout:  5 * time.Minute,
	}
	// reflect.DeepEqual treats func values as equal only when both are nil,
	// so this also checks LookupHost is nil.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("gitOptions() = %+v, want %+v", got, want)
	}
	if got.AllowFileURLs {
		t.Error("gitOptions().AllowFileURLs = true, want false: file:// URLs are for tests only")
	}
	if got.LookupHost != nil {
		t.Error("gitOptions().LookupHost is set, want nil so the system resolver is used")
	}
}

func TestGitOptions_DefaultConfig(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(writeConfig(t, ""), noEnv)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	got := gitOptions(cfg)
	if want := int64(2048) << 20; got.MaxCloneBytes != want {
		t.Errorf("MaxCloneBytes = %d, want %d", got.MaxCloneBytes, want)
	}
	if got.CloneTimeout != 30*time.Minute || got.FetchTimeout != 10*time.Minute {
		t.Errorf("timeouts = %v, %v; want 30m0s, 10m0s", got.CloneTimeout, got.FetchTimeout)
	}
	if want := filepath.Join(cfg.DataDir, "repos"); got.ReposDir != want {
		t.Errorf("ReposDir = %q, want %q", got.ReposDir, want)
	}
}

func TestIngestTargets_Tokens(t *testing.T) {
	t.Parallel()
	const targetsToken, upstreamToken = "fake-targets-token-1111", "fake-upstream-token-2222"
	cfg := config.Config{
		Auth: config.Auth{GitHubTargetsToken: targetsToken, GitHubUpstreamToken: upstreamToken},
		Targets: []config.Target{
			{Slug: "server", URL: "https://github.com/example-org/example-server", Branch: "main", Auth: "targets"},
			{Slug: "lib", URL: "https://github.com/example-org/example-lib", Auth: "upstream", AllowPrivateHosts: true},
			{Slug: "public", URL: "https://github.com/example-org/example-public", Auth: "none", Unity: true},
		},
	}
	got := ingestTargets(cfg)
	if len(got) != 3 {
		t.Fatalf("ingestTargets() returned %d targets, want 3", len(got))
	}
	tests := []struct {
		slug, url, branch string
		token             string
		allowPrivate      bool
	}{
		{slug: "server", url: "https://github.com/example-org/example-server", branch: "main", token: targetsToken},
		{slug: "lib", url: "https://github.com/example-org/example-lib", token: upstreamToken, allowPrivate: true},
		{slug: "public", url: "https://github.com/example-org/example-public", token: ""},
	}
	for i, tt := range tests {
		r := got[i]
		if r.Slug != tt.slug || r.URL != tt.url || r.Branch != tt.branch {
			t.Errorf("target %d = slug %q url %q branch %q, want %q %q %q", i, r.Slug, r.URL, r.Branch, tt.slug, tt.url, tt.branch)
		}
		if r.Token.Reveal() != tt.token {
			t.Errorf("target %s token = %q, want %q", tt.slug, r.Token.Reveal(), tt.token)
		}
		if r.AllowPrivateHost != tt.allowPrivate {
			t.Errorf("target %s AllowPrivateHost = %v, want %v", tt.slug, r.AllowPrivateHost, tt.allowPrivate)
		}
		if r.ID != 0 || r.ClonePath != "" || r.DefaultBranch != "" {
			t.Errorf("target %s has stored fields set: %+v", tt.slug, r)
		}
	}
}

func TestIngestTargets_None(t *testing.T) {
	t.Parallel()
	if got := ingestTargets(config.Config{}); len(got) != 0 {
		t.Errorf("ingestTargets(no targets) = %+v, want none", got)
	}
}

func TestTimelineTargets(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Targets: []config.Target{
		{Slug: "server", URL: "https://github.com/example-org/example-server", Auth: "targets", Ecosystems: []string{"golang", "docker"}},
		{Slug: "game", URL: "https://github.com/example-org/example-game", Auth: "none", Unity: true, ExcludePaths: []string{"examples", "tools/legacy/"}},
	}}
	want := []timeline.TargetSettings{
		{Slug: "server", Ecosystems: []string{"golang", "docker"}},
		{Slug: "game", Unity: true, ExcludePaths: []string{"examples", "tools/legacy/"}},
	}
	if got := timelineTargets(cfg); !reflect.DeepEqual(got, want) {
		t.Errorf("timelineTargets() = %+v, want %+v", got, want)
	}
}

func TestRegisteredJobs_Order(t *testing.T) {
	t.Parallel()
	// The constructors only keep the store; a nil one is never called here.
	jobs := registeredJobs(config.Config{DataDir: t.TempDir()}, nil, slog.New(slog.DiscardHandler))
	var got []string
	for _, j := range jobs {
		got = append(got, j.Name())
	}
	if want := []string{"ingest", "timeline"}; !reflect.DeepEqual(got, want) {
		t.Errorf("registered job names = %q, want %q", got, want)
	}
}

func TestScheduleEntries_IngestChainRunsTimeline(t *testing.T) {
	t.Parallel()
	var names []string
	for _, j := range registeredJobs(config.Config{DataDir: t.TempDir()}, nil, slog.New(slog.DiscardHandler)) {
		names = append(names, j.Name())
	}
	entries := scheduleEntries(config.Schedules{Ingest: "5 */6 * * *"}, names)
	if got, want := entries[0].Name, "ingest"; got != want {
		t.Fatalf("entries[0].Name = %q, want %q", got, want)
	}
	if got, want := entries[0].Jobs, []string{"ingest", "timeline"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ingest chain = %q, want %q", got, want)
	}
	for _, e := range entries[1:] {
		if len(e.Jobs) != 0 {
			t.Errorf("schedule %s jobs = %q, want none registered", e.Name, e.Jobs)
		}
	}
}
