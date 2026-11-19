package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func noEnv(string) string { return "" }

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// writeConfig writes content to a fresh upw.yaml and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upw.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// loadString writes content and loads it.
func loadString(t *testing.T, content string, getenv func(string) string) (Config, error) {
	t.Helper()
	return Load(writeConfig(t, content), getenv)
}

// wantDefaults is the Config produced by a file that sets only data_dir.
func wantDefaults(dataDir string) Config {
	return Config{
		DataDir: dataDir,
		HTTP:    HTTP{Listen: "127.0.0.1:8080"},
		Schedules: Schedules{
			Ingest: "5 */6 * * *",
			Vulns:  "10 */6 * * *",
			Issues: "15 * * * *",
			LLM:    "20 * * * *",
			Digest: "0 8 * * *",
			Backup: "30 3 * * *",
		},
		Vulns: Vulns{ExposureBasis: "published", DeriveToolchainFromImages: true},
		Upstream: Upstream{
			BackfillDays:      365,
			BackfillMaxIssues: 2000,
			PollPullRequests:  true,
		},
		Prefilter: Prefilter{Threshold: 3},
		LLM: LLM{
			Enabled:        false,
			Provider:       "anthropic",
			Model:          "claude-opus-5-5",
			Effort:         "low",
			SnippetMode:    "symbols",
			MaxSnippets:    5,
			MaxIssueChars:  12000,
			MaxCallsPerRun: 50,
			MaxUSDPerDay:   2.00,
			PricesPerMTok:  Prices{Input: 4.00, Output: 20.00},
		},
		Notify:    Notify{Heartbeat: "0 8 * * 1"},
		Retention: Retention{BackupsKeep: 7, IssuesClosedKeepDays: 730},
		Git:       Git{MaxCloneMB: 2048, MaxBlobMB: 8, CloneTimeout: 30 * time.Minute, FetchTimeout: 10 * time.Minute},
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Parallel()
	got, err := loadString(t, "data_dir: x\n", noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := wantDefaults("x"); !reflect.DeepEqual(got, want) {
		t.Errorf("Load defaults:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoad_EmptyFileUsesDefaults(t *testing.T) {
	t.Parallel()
	got, err := loadString(t, "", noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := wantDefaults("./data"); !reflect.DeepEqual(got, want) {
		t.Errorf("Load empty file:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoad_NullValuesKeepDefaults(t *testing.T) {
	t.Parallel()
	got, err := loadString(t, "http:\nschedules:\n  ingest:\nllm:\n  provider:\n", noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := wantDefaults("./data"); !reflect.DeepEqual(got, want) {
		t.Errorf("Load null values:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	t.Parallel()
	got, err := Load(filepath.Join(t.TempDir(), "absent.yaml"), noEnv)
	if err == nil {
		t.Fatal("Load(missing file) error = nil, want an error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load(missing file) error = %v, want one wrapping fs.ErrNotExist", err)
	}
	if !reflect.DeepEqual(got, Config{}) {
		t.Errorf("Load(missing file) config = %+v, want zero", got)
	}
}

func TestLoad_Example(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "deploy", "upw.example.yaml")

	cfg, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("Load(example, no env): %v", err)
	}
	if got, want := len(cfg.Targets), 3; got != want {
		t.Errorf("len(Targets) = %d, want %d", got, want)
	}
	if got, want := cfg.Upstream.Overrides["pkg:golang/toolchain"], (UpstreamOverride{Repo: "https://github.com/golang/go", WatchIssues: false}); got != want {
		t.Errorf("Overrides[pkg:golang/toolchain] = %+v, want %+v", got, want)
	}
	if got, want := len(cfg.Notify.Channels), 1; got != want {
		t.Fatalf("len(Notify.Channels) = %d, want %d", got, want)
	}
	if got, want := cfg.Notify.Channels[0].Type, "discord_webhook"; got != want {
		t.Errorf("Channels[0].Type = %q, want %q", got, want)
	}
	if got := cfg.Auth.GitHubUpstreamToken.Reveal(); got != "" {
		t.Errorf("GitHubUpstreamToken with no env = %q, want empty", got)
	}

	const token = "fake-token-abcdefgh1234"
	cfg, err = Load(path, envMap(map[string]string{"UPW_GITHUB_TOKEN_UPSTREAM": token}))
	if err != nil {
		t.Fatalf("Load(example, token set): %v", err)
	}
	if got := cfg.Auth.GitHubUpstreamToken.Reveal(); got != token {
		t.Errorf("GitHubUpstreamToken.Reveal() = %q, want %q", got, token)
	}
	if got, want := fmt.Sprint(cfg.Auth.GitHubUpstreamToken), "****1234"; got != want {
		t.Errorf("fmt.Sprint(GitHubUpstreamToken) = %q, want %q", got, want)
	}
	if got := fmt.Sprintf("%+v", cfg); strings.Contains(got, token) {
		t.Errorf("fmt.Sprintf(%%+v, cfg) contains the token: %s", got)
	}
}

// TestLoad_ExampleCommentedChannels uncomments the commented channels in the
// example and dev configs, so the examples an operator copies stay valid.
func TestLoad_ExampleCommentedChannels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file string
		want Channel
	}{
		{
			file: filepath.Join("..", "..", "deploy", "upw.example.yaml"),
			want: Channel{
				Type: "email", Host: "smtp.example.com", Port: 587, Username: "upw@example.com",
				Password: "smtp-pass", From: "upw@example.com", To: []string{"you@example.com"},
			},
		},
		{
			file: filepath.Join("..", "..", "upw.dev.yaml"),
			want: Channel{Type: "discord_webhook", URL: "https://hooks.example.com/abc"},
		},
	}
	env := envMap(map[string]string{"UPW_SMTP_PASSWORD": "smtp-pass", "UPW_WEBHOOK_URL": "https://hooks.example.com/abc"})
	for _, tt := range tests {
		t.Run(filepath.Base(tt.file), func(t *testing.T) {
			t.Parallel()
			b, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			text := strings.ReplaceAll(string(b), "    # - type:", "    - type:")
			text = strings.ReplaceAll(text, "    #   ", "      ")
			cfg, err := loadString(t, text, env)
			if err != nil {
				t.Fatalf("Load(%s uncommented): %v", tt.file, err)
			}
			chans := cfg.Notify.Channels
			if len(chans) != 2 {
				t.Fatalf("len(Notify.Channels) = %d, want 2", len(chans))
			}
			if !reflect.DeepEqual(chans[1], tt.want) {
				t.Errorf("Channels[1] = %#v, want %#v", chans[1], tt.want)
			}
		})
	}
}

func TestLoad_DevConfig(t *testing.T) {
	t.Parallel()
	cfg, err := Load(filepath.Join("..", "..", "upw.dev.yaml"), noEnv)
	if err != nil {
		t.Fatalf("Load(upw.dev.yaml): %v", err)
	}
	if got, want := cfg.DataDir, "./data-dev"; got != want {
		t.Errorf("DataDir = %q, want %q", got, want)
	}
	wantTargets := []Target{
		{Slug: "glow", URL: "https://github.com/charmbracelet/glow", Auth: "none"},
		{Slug: "cleo", URL: "https://github.com/python-poetry/cleo", Auth: "none"},
		{Slug: "naughtyattributes", URL: "https://github.com/dbrizov/NaughtyAttributes", Auth: "none", Unity: true},
		{Slug: "upstreamwatch", URL: "https://github.com/Amrit-Code/upstreamwatch", Auth: "none"},
	}
	if !reflect.DeepEqual(cfg.Targets, wantTargets) {
		t.Errorf("Targets = %+v, want %+v", cfg.Targets, wantTargets)
	}
	if cfg.LLM.Enabled {
		t.Error("LLM.Enabled = true, want false")
	}
	if got, want := cfg.Notify.Channels, []Channel{{Type: "stdout"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Notify.Channels = %+v, want %+v", got, want)
	}
}

func TestLoad_RejectsLiteralSecret(t *testing.T) {
	t.Parallel()
	const literal = "fake-token-literal"
	tests := []struct {
		name  string
		yaml  string
		field string
	}{
		{
			name:  "upstream token",
			yaml:  "auth:\n  github_upstream_token: " + literal + "\n",
			field: "auth.github_upstream_token",
		},
		{
			name:  "targets token",
			yaml:  "auth:\n  github_targets_token: " + literal + "\n",
			field: "auth.github_targets_token",
		},
		{
			name:  "llm key",
			yaml:  "llm:\n  api_key: " + literal + "\n",
			field: "llm.api_key",
		},
		{
			name:  "webhook url",
			yaml:  "notify:\n  channels:\n    - type: ntfy\n      url: " + literal + "\n",
			field: "notify.channels[0].url",
		},
		{
			name:  "email password",
			yaml:  "notify:\n  channels:\n    - {type: email, host: h, username: u, from: f, to: [t], password: " + literal + "}\n",
			field: "notify.channels[0].password",
		},
		{
			name:  "reference with a literal suffix",
			yaml:  "auth:\n  github_upstream_token: ${UPW_X}" + literal + "\n",
			field: "auth.github_upstream_token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadString(t, tt.yaml, noEnv)
			if err == nil {
				t.Fatal("Load error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.field+":") {
				t.Errorf("Load error = %q, want it to name %s", err, tt.field)
			}
			if strings.Contains(err.Error(), literal) {
				t.Errorf("Load error = %q, echoes the literal value", err)
			}
		})
	}
}

func TestLoad_SecretReferences(t *testing.T) {
	t.Parallel()
	const yaml = `llm:
  api_key: ${KEY}
notify:
  channels:
    - type: slack_webhook
      url: ${HOOK}
    - type: email
      host: smtp.example.com
      username: upw@example.com
      password: ${SMTP}
      from: upw@example.com
      to: [you@example.com]
`
	env := envMap(map[string]string{"KEY": "key-value-1", "HOOK": "https://hooks.example.com/abc", "SMTP": "smtp-pass"})
	cfg, err := loadString(t, yaml, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.LLM.APIKey, model.Secret("key-value-1"); got != want {
		t.Errorf("LLM.APIKey = %q, want %q", got.Reveal(), want.Reveal())
	}
	want := []Channel{
		{Type: "slack_webhook", URL: "https://hooks.example.com/abc"},
		{
			Type: "email", Host: "smtp.example.com", Port: 587, Username: "upw@example.com",
			Password: "smtp-pass", From: "upw@example.com", To: []string{"you@example.com"},
		},
	}
	if !reflect.DeepEqual(cfg.Notify.Channels, want) {
		t.Errorf("Notify.Channels = %#v, want %#v", cfg.Notify.Channels, want)
	}

	// An unset variable gives an empty secret, not an error.
	cfg, err = loadString(t, yaml, noEnv)
	if err != nil {
		t.Fatalf("Load with unset variables: %v", err)
	}
	if got := cfg.Notify.Channels[0].URL.Reveal(); got != "" {
		t.Errorf("Channels[0].URL with unset variable = %q, want empty", got)
	}
}

func TestLoad_TargetScheme(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want []string
	}{
		{name: "file", url: "file:///tmp/x", want: []string{"targets[0].url", `scheme "file" not allowed, only https`}},
		{name: "ssh", url: "ssh://git@github.com/example-org/example-server", want: []string{"targets[0].url", `"ssh"`}},
		{name: "git", url: "git://github.com/example-org/example-server", want: []string{"targets[0].url", `"git"`}},
		{name: "http", url: "http://github.com/example-org/example-server", want: []string{"targets[0].url", `"http"`}},
		{name: "userinfo", url: "https://user:pass@github.com/example-org/example-server", want: []string{"targets[0].url", "user info"}},
		{name: "scp style", url: "git@github.com:example-org/example-server", want: []string{"targets[0].url", "invalid URL"}},
		{name: "no host", url: "https:///example-org/example-server", want: []string{"targets[0].url", "host required"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yaml := fmt.Sprintf("targets:\n  - slug: app\n    url: %q\n", tt.url)
			_, err := loadString(t, yaml, noEnv)
			if err == nil {
				t.Fatalf("Load(url %q) error = nil, want an error", tt.url)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("Load(url %q) error = %q, want it to contain %q", tt.url, err, w)
				}
			}
			if strings.Contains(err.Error(), "pass@") {
				t.Errorf("Load(url %q) error = %q, echoes the user info", tt.url, err)
			}
		})
	}
}

func TestLoad_Targets(t *testing.T) {
	t.Parallel()
	const yaml = `targets:
  - slug: server
    url: https://github.com/example-org/example-server
    branch: main
    auth: targets
    ecosystems: [golang]
  - slug: game
    url: https://github.com/example-org/example-game
    unity: true
`
	cfg, err := loadString(t, yaml, noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []Target{
		{Slug: "server", URL: "https://github.com/example-org/example-server", Branch: "main", Auth: "targets", Ecosystems: []string{"golang"}},
		{Slug: "game", URL: "https://github.com/example-org/example-game", Auth: "none", Unity: true},
	}
	if !reflect.DeepEqual(cfg.Targets, want) {
		t.Errorf("Targets = %+v, want %+v", cfg.Targets, want)
	}
}

func TestLoad_ReportsAllProblems(t *testing.T) {
	t.Parallel()
	const yaml = `http:
  listen: not-a-host-port
targets:
  - slug: Bad_Slug
    url: https://github.com/example-org/example-server
llm:
  provider: other
`
	got, err := loadString(t, yaml, noEnv)
	if err == nil {
		t.Fatal("Load error = nil, want an error")
	}
	for _, want := range []string{"http.listen:", "targets[0].slug:", "llm.provider:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load error = %q, want it to contain %q", err, want)
		}
	}
	if !reflect.DeepEqual(got, Config{}) {
		t.Errorf("Load config on error = %+v, want zero", got)
	}
}

func TestLoad_Validation(t *testing.T) {
	t.Parallel()
	const target = "  - slug: %s\n    url: https://github.com/example-org/%s\n"
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "data_dir empty", yaml: "data_dir: \"\"\n", want: "data_dir: required"},
		{name: "listen empty", yaml: "http:\n  listen: \"\"\n", want: "http.listen: required"},
		{name: "listen port not numeric", yaml: "http:\n  listen: \"127.0.0.1:http\"\n", want: "http.listen:"},
		{name: "base_url relative", yaml: "http:\n  base_url: /upw\n", want: "http.base_url:"},
		{name: "base_url ftp", yaml: "http:\n  base_url: ftp://example.com\n", want: "http.base_url:"},
		{name: "slug missing", yaml: "targets:\n  - url: https://github.com/example-org/x\n", want: "targets[0].slug: required"},
		{name: "slug duplicate", yaml: "targets:\n" + fmt.Sprintf(target, "app", "a") + fmt.Sprintf(target, "app", "b"), want: `targets[1].slug: "app" already used by targets[0]`},
		{name: "slug leading dash", yaml: "targets:\n" + fmt.Sprintf(target, "-app", "a"), want: "targets[0].slug:"},
		{name: "url missing", yaml: "targets:\n  - slug: app\n", want: "targets[0].url: required"},
		{name: "auth unknown", yaml: "targets:\n" + fmt.Sprintf(target, "app", "a") + "    auth: token\n", want: "targets[0].auth:"},
		{name: "schedule four fields", yaml: "schedules:\n  ingest: \"5 */6 * *\"\n", want: "schedules.ingest:"},
		{name: "schedule descriptor", yaml: "schedules:\n  backup: \"@daily\"\n", want: "schedules.backup:"},
		{name: "heartbeat names", yaml: "notify:\n  heartbeat: \"0 8 * * MON\"\n", want: "notify.heartbeat:"},
		{name: "exposure basis", yaml: "vulns:\n  exposure_basis: fixed\n", want: "vulns.exposure_basis:"},
		{name: "snippet mode", yaml: "llm:\n  snippet_mode: full\n", want: "llm.snippet_mode:"},
		{name: "threshold zero", yaml: "prefilter:\n  threshold: 0\n", want: "prefilter.threshold:"},
		{name: "negative cap", yaml: "llm:\n  max_usd_per_day: -1\n", want: "llm.max_usd_per_day:"},
		{name: "negative backfill", yaml: "upstream:\n  backfill_days: -1\n", want: "upstream.backfill_days:"},
		{name: "negative retention", yaml: "retention:\n  backups_keep: -1\n", want: "retention.backups_keep:"},
		{name: "enabled without model", yaml: "llm:\n  enabled: true\n  model: \"\"\n", want: "llm.model:"},
		{name: "channel unknown type", yaml: "notify:\n  channels:\n    - type: plane\n", want: `notify.channels[0].type: unknown type "plane"`},
		{name: "channel no type", yaml: "notify:\n  channels:\n    - url: ${HOOK}\n", want: "notify.channels[0].type: required"},
		{name: "webhook without url", yaml: "notify:\n  channels:\n    - type: discord_webhook\n", want: "notify.channels[0].url: required"},
		{name: "email without host", yaml: "notify:\n  channels:\n    - {type: email, username: u, password: \"${P}\", from: f, to: [t]}\n", want: "notify.channels[0].host: required"},
		{name: "email without password", yaml: "notify:\n  channels:\n    - {type: email, host: h, username: u, from: f, to: [t]}\n", want: "notify.channels[0].password: required"},
		{name: "email without to", yaml: "notify:\n  channels:\n    - {type: email, host: h, username: u, password: \"${P}\", from: f}\n", want: "notify.channels[0].to: required"},
		{name: "email port zero", yaml: "notify:\n  channels:\n    - {type: email, host: h, port: 0, username: u, password: \"${P}\", from: f, to: [t]}\n", want: "notify.channels[0].port:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadString(t, tt.yaml, noEnv)
			if err == nil {
				t.Fatalf("Load(%q) error = nil, want one containing %q", tt.yaml, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load(%q) error = %q, want it to contain %q", tt.yaml, err, tt.want)
			}
		})
	}
}

func TestLoad_LLMEnabledWithoutKeyIsValid(t *testing.T) {
	t.Parallel()
	cfg, err := loadString(t, "llm:\n  enabled: true\n  api_key: ${UPW_LLM_API_KEY}\n", noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.LLM.Enabled || cfg.LLM.APIKey != "" {
		t.Errorf("LLM = %+v, want enabled with an empty key", cfg.LLM)
	}
}

func TestLoad_OverrideWatchIssuesDefaultsTrue(t *testing.T) {
	t.Parallel()
	cfg, err := loadString(t, "upstream:\n  overrides:\n    \"pkg:golang/example.com/lib\": { repo: https://github.com/example-org/lib }\n", noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := map[string]UpstreamOverride{"pkg:golang/example.com/lib": {Repo: "https://github.com/example-org/lib", WatchIssues: true}}
	if !reflect.DeepEqual(cfg.Upstream.Overrides, want) {
		t.Errorf("Overrides = %+v, want %+v", cfg.Upstream.Overrides, want)
	}
}

func TestLoad_UnknownKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, yaml, key string
	}{
		{name: "top level", yaml: "htpp: {}\n", key: "htpp"},
		{name: "nested", yaml: "llm:\n  modle: x\n", key: "modle"},
		{name: "in a list", yaml: "notify:\n  channels:\n    - type: stdout\n      urll: x\n", key: "urll"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadString(t, tt.yaml, noEnv)
			if err == nil {
				t.Fatalf("Load(%q) error = nil, want an error", tt.yaml)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Errorf("Load(%q) error = %q, want it to name %q", tt.yaml, err, tt.key)
			}
		})
	}
}

func TestLoad_ExpansionCannotInjectStructure(t *testing.T) {
	t.Parallel()
	const injected = "x\nhttp:\n  listen: evil"
	cfg, err := loadString(t, "data_dir: ${D}\n", envMap(map[string]string{"D": injected}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DataDir != injected {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, injected)
	}
	if got, want := cfg.HTTP.Listen, "127.0.0.1:8080"; got != want {
		t.Errorf("HTTP.Listen = %q, want %q", got, want)
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()
	env := envMap(map[string]string{"A": "1", "B_2": "two"})
	tests := []struct{ in, want string }{
		{in: "plain", want: "plain"},
		{in: "${A}", want: "1"},
		{in: "x-${A}-${B_2}", want: "x-1-two"},
		{in: "${UNSET}", want: ""},
		{in: "$A and ${not valid}", want: "$A and ${not valid}"},
	}
	for _, tt := range tests {
		if got := expand(tt.in, env); got != tt.want {
			t.Errorf("expand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsCronShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{in: "5 */6 * * *", want: true},
		{in: "0,30 8-18 * * 1-5", want: true},
		{in: "  0 8 * * 1  ", want: true},
		{in: "", want: false},
		{in: "0 8 * *", want: false},
		{in: "0 8 * * * *", want: false},
		{in: "0 8 * * MON", want: false},
		{in: "@hourly", want: false},
		{in: "0 8 ? * 1", want: false},
	}
	for _, tt := range tests {
		if got := isCronShape(tt.in); got != tt.want {
			t.Errorf("isCronShape(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
