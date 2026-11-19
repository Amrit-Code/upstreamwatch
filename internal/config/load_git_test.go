package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoad_GitDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := loadString(t, "data_dir: x\n", noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Git{MaxCloneMB: 2048, MaxBlobMB: 8, CloneTimeout: 30 * time.Minute, FetchTimeout: 10 * time.Minute}
	if cfg.Git != want {
		t.Errorf("Git = %+v, want %+v", cfg.Git, want)
	}
}

func TestLoad_GitSet(t *testing.T) {
	t.Parallel()
	const yaml = "git:\n  max_clone_mb: 512\n  max_blob_mb: 1\n  clone_timeout: 1h30m\n  fetch_timeout: ${FETCH}\n"
	cfg, err := loadString(t, yaml, envMap(map[string]string{"FETCH": "45s"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Git{MaxCloneMB: 512, MaxBlobMB: 1, CloneTimeout: 90 * time.Minute, FetchTimeout: 45 * time.Second}
	if cfg.Git != want {
		t.Errorf("Git = %+v, want %+v", cfg.Git, want)
	}
}

func TestLoad_GitValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "max_clone_mb zero", yaml: "git:\n  max_clone_mb: 0\n", want: "git.max_clone_mb: must be at least 1, got 0"},
		{name: "max_clone_mb negative", yaml: "git:\n  max_clone_mb: -5\n", want: "git.max_clone_mb: must be at least 1, got -5"},
		{name: "max_blob_mb zero", yaml: "git:\n  max_blob_mb: 0\n", want: "git.max_blob_mb: must be at least 1, got 0"},
		{name: "max_blob_mb negative", yaml: "git:\n  max_blob_mb: -1\n", want: "git.max_blob_mb: must be at least 1, got -1"},
		{name: "clone_timeout unparseable", yaml: "git:\n  clone_timeout: soon\n", want: `git.clone_timeout: "soon" is not a duration`},
		{name: "clone_timeout no unit", yaml: "git:\n  clone_timeout: \"30\"\n", want: `git.clone_timeout: "30" is not a duration`},
		{name: "clone_timeout empty", yaml: "git:\n  clone_timeout: \"\"\n", want: `git.clone_timeout: "" is not a duration`},
		{name: "clone_timeout zero", yaml: "git:\n  clone_timeout: 0s\n", want: "git.clone_timeout: must be positive, got 0s"},
		{name: "fetch_timeout unparseable", yaml: "git:\n  fetch_timeout: 10 minutes\n", want: `git.fetch_timeout: "10 minutes" is not a duration`},
		{name: "fetch_timeout negative", yaml: "git:\n  fetch_timeout: -1m\n", want: "git.fetch_timeout: must be positive, got -1m"},
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
			if n := strings.Count(err.Error(), "git."); n != 1 {
				t.Errorf("Load(%q) error = %q, want exactly one git problem, got %d", tt.yaml, err, n)
			}
			if !reflect.DeepEqual(got, Config{}) {
				t.Errorf("Load(%q) config = %+v, want zero", tt.yaml, got)
			}
		})
	}
}

func TestLoad_GitReportsEveryProblem(t *testing.T) {
	t.Parallel()
	const yaml = "git:\n  max_clone_mb: 0\n  max_blob_mb: 0\n  clone_timeout: x\n  fetch_timeout: 0s\n"
	_, err := loadString(t, yaml, noEnv)
	if err == nil {
		t.Fatal("Load error = nil, want an error")
	}
	for _, want := range []string{"git.max_clone_mb:", "git.max_blob_mb:", "git.clone_timeout:", "git.fetch_timeout:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load error = %q, want it to contain %q", err, want)
		}
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		if !pathPrefix.MatchString(line) {
			t.Errorf("error line %q does not start with a YAML path", line)
		}
	}
}

func TestLoad_AllowPrivateHosts(t *testing.T) {
	t.Parallel()
	const yaml = `targets:
  - slug: internal
    url: https://git.example.internal/example-org/example-server
    allow_private_hosts: true
  - slug: public
    url: https://github.com/example-org/example-game
`
	cfg, err := loadString(t, yaml, noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []Target{
		{Slug: "internal", URL: "https://git.example.internal/example-org/example-server", Auth: "none", AllowPrivateHosts: true},
		{Slug: "public", URL: "https://github.com/example-org/example-game", Auth: "none"},
	}
	if !reflect.DeepEqual(cfg.Targets, want) {
		t.Errorf("Targets = %+v, want %+v", cfg.Targets, want)
	}
}

func TestLoad_AllowPrivateHostsNotBool(t *testing.T) {
	t.Parallel()
	const yaml = "targets:\n  - slug: app\n    url: https://github.com/example-org/a\n    allow_private_hosts: maybe\n"
	if _, err := loadString(t, yaml, noEnv); err == nil {
		t.Errorf("Load(%q) error = nil, want a parse error", yaml)
	}
}

func TestLoad_ExcludePaths(t *testing.T) {
	t.Parallel()
	const yaml = `targets:
  - slug: app
    url: https://github.com/example-org/example-server
    exclude_paths: [examples, "tools/legacy/", "${EXTRA}"]
  - slug: other
    url: https://github.com/example-org/example-game
`
	cfg, err := loadString(t, yaml, envMap(map[string]string{"EXTRA": "docs/samples"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []Target{
		{Slug: "app", URL: "https://github.com/example-org/example-server", Auth: "none", ExcludePaths: []string{"examples", "tools/legacy/", "docs/samples"}},
		{Slug: "other", URL: "https://github.com/example-org/example-game", Auth: "none"},
	}
	if !reflect.DeepEqual(cfg.Targets, want) {
		t.Errorf("Targets = %+v, want %+v", cfg.Targets, want)
	}
}

func TestLoad_ExcludePathsValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		paths string
		want  string
	}{
		{name: "empty", paths: `[examples, ""]`, want: "targets[0].exclude_paths[1]: must not be empty"},
		{name: "leading slash", paths: `[/examples]`, want: `targets[0].exclude_paths[0]: "/examples" must be relative to the repository root, without a leading /`},
		{name: "dotdot alone", paths: `[".."]`, want: `targets[0].exclude_paths[0]: ".." must not contain a .. segment`},
		{name: "dotdot inside", paths: `[a/../b]`, want: `targets[0].exclude_paths[0]: "a/../b" must not contain a .. segment`},
		{name: "dotdot at end", paths: `[a/..]`, want: `targets[0].exclude_paths[0]: "a/.." must not contain a .. segment`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yaml := "targets:\n  - slug: app\n    url: https://github.com/example-org/a\n    exclude_paths: " + tt.paths + "\n"
			got, err := loadString(t, yaml, noEnv)
			if err == nil {
				t.Fatalf("Load(%q) error = nil, want %q", yaml, tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("Load(%q) error = %q, want %q", yaml, err, tt.want)
			}
			if !reflect.DeepEqual(got, Config{}) {
				t.Errorf("Load(%q) config = %+v, want zero", yaml, got)
			}
		})
	}
}

func TestLoad_ExcludePathsAccepted(t *testing.T) {
	t.Parallel()
	// Names that only contain dots, or ".." inside a segment, are allowed.
	const yaml = "targets:\n  - slug: app\n    url: https://github.com/example-org/a\n    exclude_paths: [examples/, a..b, ...x, .github/fixtures]\n"
	if _, err := loadString(t, yaml, noEnv); err != nil {
		t.Errorf("Load(%q) error = %v, want nil", yaml, err)
	}
}

func TestLoad_TokenHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		auth    string
		url     string
		wantErr string // empty: accepted
	}{
		{name: "targets on gitlab", auth: "targets", url: "https://gitlab.com/x/y",
			wantErr: `targets[0].auth: targets sends a GitHub token, so the host must be github.com, got "gitlab.com"; use auth none for other hosts`},
		{name: "upstream on other host", auth: "upstream", url: "https://git.example.com/example-org/lib",
			wantErr: `targets[0].auth: upstream sends a GitHub token, so the host must be github.com, got "git.example.com"`},
		{name: "targets on github subdomain", auth: "targets", url: "https://api.github.com/x/y",
			wantErr: `got "api.github.com"`},
		{name: "targets on look-alike host", auth: "targets", url: "https://github.com.example.com/x/y",
			wantErr: `got "github.com.example.com"`},
		{name: "targets on github", auth: "targets", url: "https://github.com/x/y"},
		{name: "upstream on github with port", auth: "upstream", url: "https://github.com:443/x/y"},
		{name: "none on gitlab", auth: "none", url: "https://gitlab.com/x/y"},
		{name: "none on any https host", auth: "none", url: "https://git.example.com/example-org/example-server"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yaml := "targets:\n  - slug: app\n    url: " + tt.url + "\n    auth: " + tt.auth + "\n"
			_, err := loadString(t, yaml, noEnv)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Load(%q) error = %v, want nil", yaml, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load(%q) error = %v, want it to contain %q", yaml, err, tt.wantErr)
			}
		})
	}
}

func TestLoad_TokenHostSkipsUnparseableURL(t *testing.T) {
	t.Parallel()
	// The URL problem is reported once by the URL check, not again as auth.
	yaml := "targets:\n  - slug: app\n    url: \"https://gitlab.com/%zz\"\n    auth: targets\n"
	_, err := loadString(t, yaml, noEnv)
	if err == nil || !strings.Contains(err.Error(), "targets[0].url: invalid URL") {
		t.Fatalf("Load(%q) error = %v, want the URL error", yaml, err)
	}
	if strings.Contains(err.Error(), "targets[0].auth") {
		t.Errorf("Load(%q) error = %q, want no auth error for an unparseable URL", yaml, err)
	}
}

func TestLoad_ExampleDocumentsGitAndPrivateHosts(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "deploy", "upw.example.yaml")
	cfg, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("Load(example): %v", err)
	}
	want := Git{MaxCloneMB: 2048, MaxBlobMB: 8, CloneTimeout: 30 * time.Minute, FetchTimeout: 10 * time.Minute}
	if cfg.Git != want {
		t.Errorf("example Git = %+v, want %+v", cfg.Git, want)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	for _, key := range []string{"max_clone_mb:", "max_blob_mb:", "clone_timeout:", "fetch_timeout:"} {
		if !strings.Contains(string(raw), "\n  "+key) {
			t.Errorf("example does not document git.%s", strings.TrimSuffix(key, ":"))
		}
	}
	for _, tgt := range cfg.Targets {
		if tgt.AllowPrivateHosts {
			t.Errorf("example target %q sets allow_private_hosts, want every example false", tgt.Slug)
		}
	}
}
