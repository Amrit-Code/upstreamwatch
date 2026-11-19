package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Made-up secrets; only their last four characters may appear in output.
const (
	fakeTargetsToken  = "example-targets-token-a1b2"
	fakeUpstreamToken = "example-upstream-token-c3d4"
	fakeLLMKey        = "example-llm-key-abcd"
	fakeWebhookURL    = "https://hooks.example.invalid/abcd1234wxyz"
	fakeSMTPPassword  = "example-smtp-password-e5f6"
)

// fakeChecker is a Checker that returns err and records its name in a shared
// call log.
type fakeChecker struct {
	name string
	err  error
	log  *callLog
}

func (f fakeChecker) Check(context.Context) error {
	if f.log != nil {
		f.log.add(f.name)
	}
	return f.err
}

// callLog records the order checks ran in.
type callLog struct {
	mu    sync.Mutex
	names []string
}

func (l *callLog) add(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, name)
}

func (l *callLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.names...)
}

// fixedChecks returns a check builder that returns checks whatever the
// config. It logs the config's secrets at debug level through the logger it
// is given, so the leak test also covers what reaches stderr.
func fixedChecks(checks ...namedCheck) func(config.Config, *slog.Logger) []namedCheck {
	return func(cfg config.Config, logger *slog.Logger) []namedCheck {
		logger.Debug("doctor checks built", "targets_token", cfg.Auth.GitHubTargetsToken,
			"upstream_token", cfg.Auth.GitHubUpstreamToken, "llm_key", cfg.LLM.APIKey)
		return checks
	}
}

// envOf returns a getenv over m.
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// doctor runs "upw doctor" through cmdDoctor with the given checks and
// returns the exit code run would give, stdout and stderr.
func doctor(t *testing.T, args []string, env map[string]string, checks func(config.Config, *slog.Logger) []namedCheck) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := cmdDoctor(t.Context(), args, &stdout, &stderr, envOf(env), checks)
	code := exitCode(err, &stderr)
	return code, stdout.String(), stderr.String()
}

const reportConfig = `auth:
  github_targets_token: ${UPW_GITHUB_TOKEN_TARGETS}
  github_upstream_token: ${UPW_GITHUB_TOKEN_UPSTREAM}
llm:
  enabled: true
  model: example-model
  api_key: ${UPW_LLM_API_KEY}
notify:
  channels:
    - type: stdout
    - type: discord_webhook
      url: ${UPW_WEBHOOK_URL}
`

func TestDoctor_ReportFormat(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, reportConfig)
	env := map[string]string{
		"UPW_GITHUB_TOKEN_UPSTREAM": fakeUpstreamToken,
		"UPW_LLM_API_KEY":           fakeLLMKey,
		"UPW_WEBHOOK_URL":           fakeWebhookURL,
	}
	checks := fixedChecks(
		namedCheck{name: "git", success: "installed", checker: fakeChecker{}},
		namedCheck{name: "osv", success: "api.osv.dev reachable", checker: fakeChecker{}},
		namedCheck{name: "deps.dev", success: "api.deps.dev reachable", checker: fakeChecker{}},
		namedCheck{name: "github (upstream token)", success: "api.github.com reachable", checker: fakeChecker{}},
	)
	code, stdout, stderr := doctor(t, []string{"-config", cfg}, env, checks)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr %q", code, stderr)
	}
	want := "ok    config                  " + cfg + "\n" +
		"warn  github targets token    not set: private targets cannot be fetched\n" +
		"ok    github upstream token   ****c3d4\n" +
		"ok    llm api key             ****abcd\n" +
		"ok    notify channel 2        discord_webhook ****wxyz\n" +
		"ok    git                     installed\n" +
		"ok    osv                     api.osv.dev reachable\n" +
		"ok    deps.dev                api.deps.dev reachable\n" +
		"ok    github (upstream token) api.github.com reachable\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestDoctor_NoSecretLeak(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, reportConfig+`    - type: email
      host: smtp.example.invalid
      username: upw
      password: ${UPW_SMTP_PASSWORD}
      from: upw@example.invalid
      to: [ops@example.invalid]
`)
	env := map[string]string{
		"UPW_GITHUB_TOKEN_TARGETS":  fakeTargetsToken,
		"UPW_GITHUB_TOKEN_UPSTREAM": fakeUpstreamToken,
		"UPW_LLM_API_KEY":           fakeLLMKey,
		"UPW_WEBHOOK_URL":           fakeWebhookURL,
		"UPW_SMTP_PASSWORD":         fakeSMTPPassword,
	}
	rejected := fmt.Errorf("api.github.com: token %s rejected (401)", model.Secret(fakeTargetsToken))
	checks := fixedChecks(
		namedCheck{name: "github (upstream token)", success: "api.github.com reachable", checker: fakeChecker{}},
		namedCheck{name: "github (targets token)", success: "api.github.com reachable", checker: fakeChecker{err: rejected}},
	)
	code, stdout, stderr := doctor(t, []string{"-config", cfg, "-log-level", "debug"}, env, checks)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	for _, secret := range []string{fakeTargetsToken, fakeUpstreamToken, fakeLLMKey, fakeWebhookURL, fakeSMTPPassword} {
		if strings.Contains(stdout, secret) {
			t.Errorf("stdout contains secret %q:\n%s", secret, stdout)
		}
		if strings.Contains(stderr, secret) {
			t.Errorf("stderr contains secret %q:\n%s", secret, stderr)
		}
		if last4 := "****" + secret[len(secret)-4:]; !strings.Contains(stdout, last4) {
			t.Errorf("stdout does not contain %q:\n%s", last4, stdout)
		}
	}
}

func TestDoctor_ExitCodes(t *testing.T) {
	t.Parallel()
	allSet := map[string]string{
		"UPW_GITHUB_TOKEN_TARGETS":  fakeTargetsToken,
		"UPW_GITHUB_TOKEN_UPSTREAM": fakeUpstreamToken,
		"UPW_LLM_API_KEY":           fakeLLMKey,
		"UPW_WEBHOOK_URL":           fakeWebhookURL,
	}
	tests := []struct {
		name      string
		env       map[string]string
		failAt    string // the check that fails, if any
		wantCode  int
		wantLines []string
	}{
		{name: "all ok", env: allSet, wantCode: 0},
		{name: "warnings only", env: map[string]string{}, wantCode: 0,
			wantLines: []string{"warn  github targets token", "warn  github upstream token", "warn  llm api key", "warn  notify channel 2        discord_webhook secret not set"}},
		{name: "one fail", env: allSet, failAt: "osv", wantCode: 1,
			wantLines: []string{"fail  osv                     api.osv.dev: status 500", "ok    deps.dev", "ok    github"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := writeConfig(t, reportConfig)
			log := &callLog{}
			var checks []namedCheck
			for _, name := range []string{"git", "osv", "deps.dev", "github"} {
				f := fakeChecker{name: name, log: log}
				if name == tt.failAt {
					f.err = errors.New("api.osv.dev: status 500")
				}
				checks = append(checks, namedCheck{name: name, success: name + " reachable", checker: f})
			}
			code, stdout, stderr := doctor(t, []string{"-config", cfg}, tt.env, fixedChecks(checks...))
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d; stdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}
			if got, want := log.get(), []string{"git", "osv", "deps.dev", "github"}; !reflect.DeepEqual(got, want) {
				t.Errorf("checks ran %q, want %q", got, want)
			}
			for _, line := range tt.wantLines {
				if !strings.Contains(stdout, "\n"+line) {
					t.Errorf("stdout has no line starting %q:\n%s", line, stdout)
				}
			}
		})
	}
}

func TestDoctor_FailureIsTerminalSafe(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	const raw = "api.example.invalid: \x1b]0;owned\x07 status 500"
	checks := fixedChecks(namedCheck{name: "osv", success: "api.osv.dev reachable", checker: fakeChecker{err: errors.New(raw)}})
	code, stdout, _ := doctor(t, []string{"-config", cfg}, nil, checks)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "fail  osv                     " + strconv.Quote(raw) + "\n"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, want)
	}
	if strings.ContainsAny(stdout, "\x1b\x07") {
		t.Errorf("stdout holds a raw control character: %q", stdout)
	}
}

func TestDoctor_BadConfig(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "prefilter:\n  threshold: 0\nauth:\n  github_targets_token: literal-token-value\n")
	var stdout, stderr bytes.Buffer
	// Through run: no check runs, because the config fails before any is built.
	if got, want := run(t.Context(), []string{"doctor", "-config", cfg}, &stdout, &stderr, noEnv), 1; got != want {
		t.Errorf("run(doctor) = %d, want %d", got, want)
	}
	errOut := stderr.String()
	if !strings.Contains(errOut, "prefilter.threshold") || !strings.Contains(errOut, "auth.github_targets_token") {
		t.Errorf("stderr = %q, want both config problems", errOut)
	}
	if strings.Contains(errOut, "literal-token-value") {
		t.Errorf("stderr = %q, repeats the literal secret", errOut)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
}

func TestDoctor_UsageErrors(t *testing.T) {
	t.Parallel()
	never := func(config.Config, *slog.Logger) []namedCheck {
		t.Error("checks built on a usage error")
		return nil
	}
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{name: "help", args: []string{"-h"}, wantCode: 0, wantOut: "usage: upw doctor"},
		{name: "extra argument", args: []string{"extra"}, wantCode: 2},
		{name: "unknown flag", args: []string{"-bogus"}, wantCode: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, stdout, _ := doctor(t, tt.args, nil, never)
			if code != tt.wantCode {
				t.Errorf("doctor(%q) exit code = %d, want %d", tt.args, code, tt.wantCode)
			}
			if !strings.HasPrefix(stdout, tt.wantOut) {
				t.Errorf("doctor(%q) stdout = %q, want prefix %q", tt.args, stdout, tt.wantOut)
			}
		})
	}
}

func TestUsage_ListsDoctor(t *testing.T) {
	t.Parallel()
	if !strings.Contains(usage, "\n  doctor ") {
		t.Errorf("usage does not list doctor:\n%s", usage)
	}
}

func TestDoctorChecks_Names(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	tests := []struct {
		name string
		auth config.Auth
		want []string
	}{
		{name: "no tokens", want: []string{"git", "osv", "deps.dev", "github"}},
		{name: "upstream only", auth: config.Auth{GitHubUpstreamToken: fakeUpstreamToken},
			want: []string{"git", "osv", "deps.dev", "github (upstream token)"}},
		{name: "targets only", auth: config.Auth{GitHubTargetsToken: fakeTargetsToken},
			want: []string{"git", "osv", "deps.dev", "github (targets token)"}},
		{name: "both", auth: config.Auth{GitHubTargetsToken: fakeTargetsToken, GitHubUpstreamToken: fakeUpstreamToken},
			want: []string{"git", "osv", "deps.dev", "github (upstream token)", "github (targets token)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, c := range doctorChecks(config.Config{Auth: tt.auth}, logger) {
				got = append(got, c.name)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("doctorChecks(%s) names = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestSecretLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  config.Config
		want []reportLine
	}{
		{
			name: "short secrets fully masked, llm disabled",
			cfg: config.Config{
				Auth: config.Auth{GitHubTargetsToken: "12345678", GitHubUpstreamToken: "123456789"},
				LLM:  config.LLM{APIKey: fakeLLMKey},
			},
			want: []reportLine{
				{statusOK, "github targets token", "****"},
				{statusOK, "github upstream token", "****6789"},
			},
		},
		{
			name: "llm enabled without key, channels with and without secrets",
			cfg: config.Config{
				LLM: config.LLM{Enabled: true},
				Notify: config.Notify{Channels: []config.Channel{
					{Type: "stdout"},
					{Type: "email", Password: fakeSMTPPassword},
					{Type: "ntfy"},
					{Type: "slack_webhook", URL: fakeWebhookURL},
				}},
			},
			want: []reportLine{
				{statusWarn, "github targets token", "not set: private targets cannot be fetched"},
				{statusWarn, "github upstream token", "not set: upstream polling limited to 60 requests an hour"},
				{statusWarn, "llm api key", "not set: add it on the settings page or set UPW_LLM_API_KEY"},
				{statusOK, "notify channel 2", "email ****e5f6"},
				{statusWarn, "notify channel 3", "ntfy secret not set"},
				{statusOK, "notify channel 4", "slack_webhook ****wxyz"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := secretLines(tt.cfg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("secretLines() =\n%v\nwant\n%v", got, tt.want)
			}
		})
	}
}
