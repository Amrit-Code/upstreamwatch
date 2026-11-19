package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"mime/quotedprintable"
	"path/filepath"
	"reflect"
	"strings"
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
		Git:     config.Git{MaxCloneMB: 512, MaxBlobMB: 3, CloneTimeout: 45 * time.Minute, FetchTimeout: 5 * time.Minute},
	}
	got := gitOptions(cfg)
	want := gitsrc.Options{
		ReposDir:      filepath.Join(dataDir, "repos"),
		HomeDir:       filepath.Join(dataDir, "git-home"),
		MaxCloneBytes: 512 * 1024 * 1024,
		MaxBlobBytes:  3 * 1024 * 1024,
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
	if want := int64(8) << 20; got.MaxBlobBytes != want {
		t.Errorf("MaxBlobBytes = %d, want %d", got.MaxBlobBytes, want)
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
	dataDir := t.TempDir()
	jobs, err := registeredJobs(config.Config{DataDir: dataDir}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("registeredJobs() error = %v", err)
	}
	var got []string
	for _, j := range jobs {
		got = append(got, j.Name())
		lj, ok := j.(*lockedJob)
		if !ok {
			t.Errorf("job %s is %T, want it wrapped in its lock file", j.Name(), j)
			continue
		}
		if want := filepath.Join(dataDir, "locks"); lj.dir != want {
			t.Errorf("job %s lock directory = %q, want %q", j.Name(), lj.dir, want)
		}
	}
	if want := []string{"ingest", "timeline", "vulns", "digest", "backup"}; !reflect.DeepEqual(got, want) {
		t.Errorf("registered job names = %q, want %q", got, want)
	}
}

func TestRegisteredJobs_ChannelErrorNamesChannel(t *testing.T) {
	t.Parallel()
	bad := validEmail()
	bad.From = "not an address"
	cfg := config.Config{DataDir: t.TempDir(), Notify: config.Notify{Channels: []config.Channel{bad}}}
	jobs, err := registeredJobs(cfg, nil, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatalf("registeredJobs() = %d jobs, nil; want the channel error", len(jobs))
	}
	if msg := err.Error(); !strings.Contains(msg, "notify channel email:") || strings.Contains(msg, fakeSMTPPassword) {
		t.Errorf("registeredJobs() error = %q, want it to name channel email and not the password", msg)
	}
}

func TestStart_InvalidChannelFailsStartup(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, `notify:
  channels:
    - type: email
      host: smtp.example.invalid
      username: upw
      password: ${UPW_SMTP_PASSWORD}
      from: "not an address"
      to: [ops@example.invalid]
`)
	var stdout, stderr bytes.Buffer
	args := []string{"run", "digest", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, envOf(map[string]string{"UPW_SMTP_PASSWORD": fakeSMTPPassword})), 1; got != want {
		t.Errorf("run(%q) = %d, want %d", args, got, want)
	}
	errOut := stderr.String()
	if !strings.Contains(errOut, "upw: notify channel email:") {
		t.Errorf("stderr = %q, want the channel named", errOut)
	}
	if strings.Contains(errOut, fakeSMTPPassword) || strings.Contains(stdout.String(), fakeSMTPPassword) {
		t.Errorf("output contains the password: stderr %q", errOut)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing run", stdout.String())
	}
}

func TestHeartbeatLastFire(t *testing.T) {
	t.Parallel()
	monday8 := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	got, ok := heartbeatLastFire("0 8 * * 1", slog.New(slog.DiscardHandler))(monday8.Add(time.Hour))
	if !ok || !got.Equal(monday8) {
		t.Errorf("heartbeat(weekly) = %v, %v; want %v, true", got, ok, monday8)
	}
	var logs bytes.Buffer
	got, ok = heartbeatLastFire("@weekly", slog.New(slog.NewTextHandler(&logs, nil)))(monday8)
	if ok || !got.IsZero() {
		t.Errorf("heartbeat(@weekly) = %v, %v; want zero, false", got, ok)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "heartbeat schedule not usable") {
		t.Errorf("logs = %q, want a warning about the heartbeat schedule", logs.String())
	}
}

func TestScheduleEntries_IngestChainRunsTimeline(t *testing.T) {
	t.Parallel()
	jobs, err := registeredJobs(config.Config{DataDir: t.TempDir()}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("registeredJobs() error = %v", err)
	}
	var names []string
	for _, j := range jobs {
		names = append(names, j.Name())
	}
	entries := scheduleEntries(config.Schedules{Ingest: "5 */6 * * *"}, names)
	want := map[string][]string{
		"ingest": {"ingest", "timeline"},
		"vulns":  {"vulns"},
		"issues": nil,
		"llm":    nil,
		"digest": {"digest"},
		"backup": {"backup"},
	}
	if len(entries) != len(want) {
		t.Fatalf("scheduleEntries() returned %d entries, want %d", len(entries), len(want))
	}
	for _, e := range entries {
		if w, ok := want[e.Name]; !ok || !reflect.DeepEqual(e.Jobs, w) {
			t.Errorf("schedule %s jobs = %q, want %q", e.Name, e.Jobs, w)
		}
	}
}

// TestRunDigest_StdoutAndEmail is not parallel: it swaps notifyStdout, which
// parallel tests read, and parallel tests only start once every serial test
// has returned.
func TestRunDigest_StdoutAndEmail(t *testing.T) {
	smtpSrv := startFakeSMTP(t)
	cfg := writeConfig(t, fmt.Sprintf(`notify:
  channels:
    - type: stdout
    - type: email
      host: 127.0.0.1
      port: %d
      username: upw
      password: ${UPW_SMTP_PASSWORD}
      from: upw@example.invalid
      to: [ops@example.invalid]
`, smtpSrv.port()))
	seedFindings(t, cfg)

	var digestOut bytes.Buffer
	saved := notifyStdout
	notifyStdout = &digestOut
	t.Cleanup(func() { notifyStdout = saved })

	var stdout, stderr bytes.Buffer
	args := []string{"run", "digest", "-config", cfg}
	env := envOf(map[string]string{"UPW_SMTP_PASSWORD": fakeSMTPPassword})
	if got, want := run(t.Context(), args, &stdout, &stderr, env), 0; got != want {
		t.Fatalf("run(%q) = %d, want %d; stderr %q", args, got, want, stderr.String())
	}
	if line := stdout.String(); !strings.HasPrefix(line, "digest: succeeded") || !strings.Contains(line, " sent=2 ") {
		t.Errorf("stdout = %q, want the digest stats line with sent=2", line)
	}

	printed := digestOut.String()
	if !strings.Contains(printed, "GO-2026-0001") {
		t.Errorf("stdout channel wrote %q, want the seeded advisory in it", printed)
	}
	msgs := smtpSrv.messages()
	if len(msgs) != 1 {
		t.Fatalf("SMTP server received %d messages, want 1", len(msgs))
	}
	_, body, ok := strings.Cut(msgs[0], "\r\n\r\n")
	if !ok {
		t.Fatalf("message has no header/body separator: %q", msgs[0])
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("decode quoted-printable body: %v", err)
	}
	emailed := strings.ReplaceAll(string(decoded), "\r\n", "\n")
	if strings.TrimRight(emailed, "\n") != strings.TrimRight(printed, "\n") {
		t.Errorf("emailed text differs from the printed digest\nemailed:\n%s\nprinted:\n%s", emailed, printed)
	}
	if strings.Contains(stderr.String(), fakeSMTPPassword) {
		t.Errorf("stderr contains the SMTP password: %q", stderr.String())
	}
}
