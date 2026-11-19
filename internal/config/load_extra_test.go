package config

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Gap tests for WO-03: validation rules with no case yet, values that must be
// accepted, the YAML-path prefix on every error line, expansion in values only
// (including lists and validated fields), and the zero Config on a parse error.

func TestLoad_ValidationExtra(t *testing.T) {
	t.Parallel()
	const email = "notify:\n  channels:\n    - {type: email, host: h, username: u, password: \"${P}\", from: f, to: [t]%s}\n"
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "schedule vulns", yaml: "schedules:\n  vulns: \"10 */6 * *\"\n", want: "schedules.vulns:"},
		{name: "schedule issues", yaml: "schedules:\n  issues: \"@hourly\"\n", want: "schedules.issues:"},
		{name: "schedule llm", yaml: "schedules:\n  llm: \"20 * * * * *\"\n", want: "schedules.llm:"},
		{name: "schedule digest", yaml: "schedules:\n  digest: \"0 8 * * MON\"\n", want: "schedules.digest:"},
		{name: "schedule empty", yaml: "schedules:\n  ingest: \"\"\n", want: "schedules.ingest:"},
		{name: "negative backfill_max_issues", yaml: "upstream:\n  backfill_max_issues: -1\n", want: "upstream.backfill_max_issues:"},
		{name: "negative max_snippets", yaml: "llm:\n  max_snippets: -1\n", want: "llm.max_snippets:"},
		{name: "negative max_issue_chars", yaml: "llm:\n  max_issue_chars: -1\n", want: "llm.max_issue_chars:"},
		{name: "negative max_calls_per_run", yaml: "llm:\n  max_calls_per_run: -1\n", want: "llm.max_calls_per_run:"},
		{name: "negative input price", yaml: "llm:\n  prices_per_mtok:\n    input: -0.5\n", want: "llm.prices_per_mtok.input:"},
		{name: "negative output price", yaml: "llm:\n  prices_per_mtok:\n    output: -0.5\n", want: "llm.prices_per_mtok.output:"},
		{name: "negative issues_closed_keep_days", yaml: "retention:\n  issues_closed_keep_days: -1\n", want: "retention.issues_closed_keep_days:"},
		{name: "negative threshold", yaml: "prefilter:\n  threshold: -2\n", want: "prefilter.threshold:"},
		{name: "provider unknown", yaml: "llm:\n  provider: openai\n", want: "llm.provider:"},
		{name: "slack without url", yaml: "notify:\n  channels:\n    - type: slack_webhook\n", want: "notify.channels[0].url: required"},
		{name: "ntfy without url", yaml: "notify:\n  channels:\n    - type: ntfy\n", want: "notify.channels[0].url: required"},
		{name: "email without username", yaml: "notify:\n  channels:\n    - {type: email, host: h, password: \"${P}\", from: f, to: [t]}\n", want: "notify.channels[0].username: required"},
		{name: "email without from", yaml: "notify:\n  channels:\n    - {type: email, host: h, username: u, password: \"${P}\", to: [t]}\n", want: "notify.channels[0].from: required"},
		{name: "email port too high", yaml: strings.Replace(email, "%s", ", port: 65536", 1), want: "notify.channels[0].port:"},
		{name: "email port negative", yaml: strings.Replace(email, "%s", ", port: -1", 1), want: "notify.channels[0].port:"},
		{name: "email empty recipient", yaml: "notify:\n  channels:\n    - {type: email, host: h, username: u, password: \"${P}\", from: f, to: [a, \"\"]}\n", want: "notify.channels[0].to[1]:"},
		{name: "second channel index", yaml: "notify:\n  channels:\n    - type: stdout\n    - type: ntfy\n", want: "notify.channels[1].url: required"},
		{name: "listen without port", yaml: "http:\n  listen: \"127.0.0.1\"\n", want: "http.listen:"},
		{name: "listen port out of range", yaml: "http:\n  listen: \"127.0.0.1:70000\"\n", want: "http.listen:"},
		{name: "base_url without host", yaml: "http:\n  base_url: \"https://\"\n", want: "http.base_url:"},
		{name: "second target index", yaml: "targets:\n  - slug: a\n    url: https://github.com/example-org/a\n  - slug: b\n    url: http://github.com/example-org/b\n", want: `targets[1].url: scheme "http" not allowed, only https`},
		{name: "slug uppercase", yaml: "targets:\n  - slug: App\n    url: https://github.com/example-org/a\n", want: "targets[0].slug:"},
		{name: "slug with underscore", yaml: "targets:\n  - slug: my_app\n    url: https://github.com/example-org/a\n", want: "targets[0].slug:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := loadString(t, tt.yaml, noEnv)
			if err == nil {
				t.Fatalf("Load(%q) error = nil, want one containing %q", tt.yaml, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load(%q) error = %q, want it to contain %q", tt.yaml, err, tt.want)
			}
			if !reflect.DeepEqual(got, Config{}) {
				t.Errorf("Load(%q) config on error = %+v, want zero", tt.yaml, got)
			}
		})
	}
}

func TestLoad_AcceptsEveryAllowedValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		yaml  string
		check func(Config) bool
	}{
		{name: "exposure basis pin", yaml: "vulns:\n  exposure_basis: pin\n", check: func(c Config) bool { return c.Vulns.ExposureBasis == "pin" }},
		{name: "provider openai_compat", yaml: "llm:\n  provider: openai_compat\n  base_url: https://llm.example.com/v1\n", check: func(c Config) bool {
			return c.LLM.Provider == "openai_compat" && c.LLM.BaseURL == "https://llm.example.com/v1"
		}},
		{name: "snippet mode snippets", yaml: "llm:\n  snippet_mode: snippets\n", check: func(c Config) bool { return c.LLM.SnippetMode == "snippets" }},
		{name: "snippet mode none", yaml: "llm:\n  snippet_mode: none\n", check: func(c Config) bool { return c.LLM.SnippetMode == "none" }},
		{name: "auth upstream", yaml: "targets:\n  - slug: lib-1\n    url: https://github.com/example-org/lib\n    auth: upstream\n", check: func(c Config) bool {
			return len(c.Targets) == 1 && c.Targets[0].Auth == "upstream"
		}},
		{name: "auth none explicit", yaml: "targets:\n  - slug: lib\n    url: https://github.com/example-org/lib\n    auth: none\n", check: func(c Config) bool {
			return len(c.Targets) == 1 && c.Targets[0].Auth == "none"
		}},
		{name: "base_url http", yaml: "http:\n  base_url: http://upw.example.ts.net:8080\n", check: func(c Config) bool { return c.HTTP.BaseURL == "http://upw.example.ts.net:8080" }},
		{name: "base_url https with path", yaml: "http:\n  base_url: https://upw.example.ts.net/upw\n", check: func(c Config) bool { return c.HTTP.BaseURL == "https://upw.example.ts.net/upw" }},
		{name: "listen ipv6", yaml: "http:\n  listen: \"[::1]:9090\"\n", check: func(c Config) bool { return c.HTTP.Listen == "[::1]:9090" }},
		{name: "zero caps are not negative", yaml: "llm:\n  max_calls_per_run: 0\n  max_usd_per_day: 0\nretention:\n  backups_keep: 0\n", check: func(c Config) bool {
			return c.LLM.MaxCallsPerRun == 0 && c.LLM.MaxUSDPerDay == 0 && c.Retention.BackupsKeep == 0
		}},
		{name: "every channel type", yaml: `notify:
  channels:
    - type: stdout
    - type: ntfy
      url: ${N}
    - type: slack_webhook
      url: ${S}
    - type: discord_webhook
      url: ${D}
    - {type: email, host: smtp.example.com, port: 465, username: u, password: "${P}", from: upw@example.com, to: [a@example.com, b@example.com]}
`, check: func(c Config) bool {
			ch := c.Notify.Channels
			return len(ch) == 5 && ch[4].Port == 465 && len(ch[4].To) == 2
		}},
		{name: "custom cron schedules", yaml: "schedules:\n  ingest: \"0,30 8-18 * * 1-5\"\n  backup: \"*/15 * * * *\"\nnotify:\n  heartbeat: \"0 9 * * 0\"\n", check: func(c Config) bool {
			return c.Schedules.Ingest == "0,30 8-18 * * 1-5" && c.Schedules.Backup == "*/15 * * * *" && c.Notify.Heartbeat == "0 9 * * 0"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := loadString(t, tt.yaml, noEnv)
			if err != nil {
				t.Fatalf("Load(%q) error = %v, want nil", tt.yaml, err)
			}
			if !tt.check(cfg) {
				t.Errorf("Load(%q) = %+v, values not as written", tt.yaml, cfg)
			}
		})
	}
}

// pathPrefix is a YAML path such as data_dir, http.listen, targets[0].url or
// notify.channels[1].to[0], followed by ": ".
var pathPrefix = regexp.MustCompile(`^[a-z_]+(\[\d+\])?(\.[a-z_]+(\[\d+\])?)*: `)

func TestLoad_EveryErrorLineStartsWithPath(t *testing.T) {
	t.Parallel()
	const yaml = `data_dir: ""
http:
  listen: nope
  base_url: /relative
auth:
  github_targets_token: literal-token-value
targets:
  - slug: Bad
    url: ssh://git@github.com/example-org/a
    auth: token
  - url: ""
schedules:
  digest: "@daily"
vulns:
  exposure_basis: fixed
prefilter:
  threshold: 0
llm:
  enabled: true
  model: ""
  provider: other
  snippet_mode: full
  max_usd_per_day: -1
notify:
  heartbeat: weekly
  channels:
    - type: plane
    - type: email
retention:
  backups_keep: -3
`
	_, err := loadString(t, yaml, noEnv)
	if err == nil {
		t.Fatal("Load error = nil, want an error")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) < 20 {
		t.Errorf("Load reported %d problems, want every one of at least 20:\n%s", len(lines), err)
	}
	for _, line := range lines {
		if !pathPrefix.MatchString(line) {
			t.Errorf("error line %q does not start with a YAML path", line)
		}
	}
	for _, want := range []string{
		"data_dir:", "http.listen:", "http.base_url:", "auth.github_targets_token:",
		"targets[0].slug:", "targets[0].url:", "targets[0].auth:", "targets[1].slug:", "targets[1].url:",
		"schedules.digest:", "vulns.exposure_basis:", "prefilter.threshold:", "llm.model:", "llm.provider:",
		"llm.snippet_mode:", "llm.max_usd_per_day:", "notify.heartbeat:", "notify.channels[0].type:",
		"notify.channels[1].host:", "retention.backups_keep:",
	} {
		if !strings.Contains(err.Error(), "\n"+want) && !strings.HasPrefix(err.Error(), want) {
			t.Errorf("Load error has no line starting %q:\n%s", want, err)
		}
	}
	if strings.Contains(err.Error(), "literal-token-value") {
		t.Errorf("Load error echoes the literal secret")
	}
}

func TestLoad_ExpandsValuesOnly(t *testing.T) {
	t.Parallel()

	t.Run("strings, lists and nested values", func(t *testing.T) {
		t.Parallel()
		const yaml = `targets:
  - slug: ${SLUG}
    url: ${TARGET_URL}
    branch: ${BRANCH}
    ecosystems: [golang, "${ECO}"]
upstream:
  overrides:
    "pkg:golang/example.com/lib": { repo: "${LIB_REPO}" }
notify:
  channels:
    - {type: email, host: "${SMTP_HOST}", username: u, password: "${P}", from: f, to: ["${TO}"]}
`
		env := envMap(map[string]string{
			"SLUG": "server", "TARGET_URL": "https://github.com/example-org/example-server", "BRANCH": "main",
			"ECO": "npm", "LIB_REPO": "https://github.com/example-org/lib", "SMTP_HOST": "smtp.example.com",
			"P": "pw", "TO": "you@example.com",
		})
		cfg, err := loadString(t, yaml, env)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTarget := Target{Slug: "server", URL: "https://github.com/example-org/example-server", Branch: "main", Auth: "none", Ecosystems: []string{"golang", "npm"}}
		if len(cfg.Targets) != 1 || !reflect.DeepEqual(cfg.Targets[0], wantTarget) {
			t.Errorf("Targets = %+v, want [%+v]", cfg.Targets, wantTarget)
		}
		if got := cfg.Upstream.Overrides["pkg:golang/example.com/lib"].Repo; got != "https://github.com/example-org/lib" {
			t.Errorf("override repo = %q, want the expanded value", got)
		}
		if ch := cfg.Notify.Channels; len(ch) != 1 || ch[0].Host != "smtp.example.com" || !reflect.DeepEqual(ch[0].To, []string{"you@example.com"}) {
			t.Errorf("Notify.Channels = %+v, want host and to expanded", ch)
		}
	})

	t.Run("validation sees the expanded value", func(t *testing.T) {
		t.Parallel()
		_, err := loadString(t, "targets:\n  - slug: app\n    url: ${TARGET_URL}\n",
			envMap(map[string]string{"TARGET_URL": "file:///etc/passwd"}))
		if err == nil || !strings.Contains(err.Error(), `targets[0].url: scheme "file" not allowed`) {
			t.Errorf("Load(url from env = file://...) error = %v, want the scheme rejected", err)
		}
	})

	t.Run("a required value from an unset variable is reported", func(t *testing.T) {
		t.Parallel()
		_, err := loadString(t, "data_dir: ${UNSET_DIR}\n", noEnv)
		if err == nil || !strings.Contains(err.Error(), "data_dir: required") {
			t.Errorf("Load(data_dir from unset variable) error = %v, want data_dir: required", err)
		}
	})

	t.Run("keys are not expanded", func(t *testing.T) {
		t.Parallel()
		_, err := loadString(t, "${KEY}: x\n", envMap(map[string]string{"KEY": "data_dir"}))
		if err == nil {
			t.Errorf("Load(${KEY}: x) error = nil, want an unknown-key error: only values are expanded")
		}
	})
}

func TestLoad_SecretMustBeExactlyOneReference(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"${A}${B}", "$UPW_TOKEN", "prefix-${A}", "${A} ", "{A}"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			yaml := "auth:\n  github_upstream_token: \"" + raw + "\"\n"
			_, err := loadString(t, yaml, envMap(map[string]string{"A": "value-a-1234", "B": "value-b"}))
			if err == nil {
				t.Fatalf("Load(token %q) error = nil, want an error", raw)
			}
			if !strings.Contains(err.Error(), "auth.github_upstream_token:") {
				t.Errorf("Load(token %q) error = %q, want it to name the field", raw, err)
			}
			if strings.Contains(err.Error(), "value-a-1234") {
				t.Errorf("Load(token %q) error = %q, echoes the variable's value", raw, err)
			}
		})
	}
}

func TestLoad_InvalidTargetURLNotEchoed(t *testing.T) {
	t.Parallel()
	const yaml = "targets:\n  - slug: app\n    url: \"https://secret-token%zz@github.com/example-org/app\"\n"
	_, err := loadString(t, yaml, noEnv)
	if err == nil {
		t.Fatal("Load(unparseable url) error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "targets[0].url: invalid URL") {
		t.Errorf("Load(unparseable url) error = %q, want it to name targets[0].url", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Errorf("Load(unparseable url) error = %q, repeats the URL's user info", err)
	}
}

func TestLoad_ParseErrorsReturnZeroConfig(t *testing.T) {
	t.Parallel()
	for name, yaml := range map[string]string{
		"malformed":   "data_dir: [unclosed\n",
		"wrong type":  "http:\n  listen: [a, b]\n",
		"unknown key": "data_dir: x\nhttp:\n  lissen: y\n",
		"not a map":   "- just\n- a list\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := loadString(t, yaml, noEnv)
			if err == nil {
				t.Fatalf("Load(%q) error = nil, want an error", yaml)
			}
			if !reflect.DeepEqual(got, Config{}) {
				t.Errorf("Load(%q) config = %+v, want zero", yaml, got)
			}
		})
	}
}
