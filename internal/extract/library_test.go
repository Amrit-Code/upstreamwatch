package extract

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const requirementsSample = `# runtime
requests==2.31.0
click>=6
flask
legacy===1.0
wild==1.*
ranged>=1,<2
spaced == 3.0
Requests==2.32.0
`

const pyprojectSample = `[project]
name = "example-app"
version = "0.1.0"
dependencies = ["requests==2.31.0", "click>=6"]

[project.optional-dependencies]
dev = ["pytest==8.3.3"]
`

const workflowSample = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@0aaccfd150d50ccaeb58ebd88d36e91967a5f35b
      - uses: ./.github/actions/local
      - uses: docker://alpine:3.20
      - uses: actions/checkout@v3
      - uses: example-org/example-action/sub@main
`

func pypiDep(name, purlName, version, requirement string, direct model.Directness, scope string) model.Dependency {
	return model.Dependency{
		Package:     model.Package{PURL: "pkg:pypi/" + purlName, Ecosystem: "pypi", Name: name},
		Version:     version,
		Requirement: requirement,
		Direct:      direct,
		Scope:       scope,
	}
}

func TestParse_Python(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    string
		content []byte
		want    model.ParsedManifest
	}{
		{
			path:    "requirements.txt",
			content: []byte(requirementsSample),
			want: model.ParsedManifest{Ecosystem: "pypi", Kind: model.ManifestKindManifest, Dependencies: []model.Dependency{
				pypiDep("requests", "requests", "2.31.0", "==2.31.0", model.DirectYes, "runtime"),
				pypiDep("click", "click", "", ">=6", model.DirectYes, "runtime"),
				pypiDep("flask", "flask", "", "", model.DirectYes, "runtime"),
				pypiDep("legacy", "legacy", "", "===1.0", model.DirectYes, "runtime"),
				pypiDep("wild", "wild", "", "==1.*", model.DirectYes, "runtime"),
				pypiDep("ranged", "ranged", "", ">=1,<2", model.DirectYes, "runtime"),
				pypiDep("spaced", "spaced", "3.0", "== 3.0", model.DirectYes, "runtime"),
				// Requests==2.32.0 has the same package URL as requests and is dropped.
			}},
		},
		{
			path:    "svc/pyproject.toml",
			content: []byte(pyprojectSample),
			want: model.ParsedManifest{Ecosystem: "pypi", Kind: model.ManifestKindManifest, Dependencies: []model.Dependency{
				pypiDep("requests", "requests", "2.31.0", "==2.31.0", model.DirectYes, "runtime"),
				pypiDep("click", "click", "", ">=6", model.DirectYes, "runtime"),
				pypiDep("pytest", "pytest", "8.3.3", "==8.3.3", model.DirectYes, "development"),
			}},
		},
		{
			path:    "svc/uv.lock",
			content: readTestdata(t, "uv.lock"),
			want: model.ParsedManifest{Ecosystem: "pypi", Kind: model.ManifestKindLockfile, Dependencies: []model.Dependency{
				pypiDep("certifi", "certifi", "2025.8.3", "2025.8.3", model.DirectUnknown, "runtime"),
				// example-app, the project itself with an editable source,
				// is dropped.
				pypiDep("requests", "requests", "2.32.5", "2.32.5", model.DirectUnknown, "runtime"),
			}},
		},
		{
			path:    "poetry.lock",
			content: readTestdata(t, "poetry.lock"),
			want: model.ParsedManifest{Ecosystem: "pypi", Kind: model.ManifestKindLockfile, Dependencies: []model.Dependency{
				pypiDep("click", "click", "8.1.7", "8.1.7", model.DirectUnknown, "runtime"),
				pypiDep("pytest", "pytest", "8.3.3", "8.3.3", model.DirectUnknown, "runtime"),
				// The directory and file packages are dropped; the git one
				// is kept, its repository_url qualifier stripped.
				pypiDep("example-git", "example-git", "1.0.0", "1.0.0", model.DirectUnknown, "runtime"),
			}},
		},
	}
	e := newExtractor(true)
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got, err := e.Parse(tt.path, tt.content)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.path, err)
			}
			checkParsed(t, tt.path, got, tt.want)
		})
	}
}

func TestParse_Actions(t *testing.T) {
	t.Parallel()
	action := func(name, purlName, ref string) model.Dependency {
		return model.Dependency{
			Package:     model.Package{PURL: "pkg:githubactions/" + purlName, Ecosystem: "github-actions", Name: name},
			Version:     ref,
			Requirement: ref,
			Direct:      model.DirectYes,
			Scope:       "runtime",
		}
	}
	want := model.ParsedManifest{Ecosystem: "github-actions", Kind: model.ManifestKindManifest, Dependencies: []model.Dependency{
		action("actions/checkout", "actions/checkout", "v4"),
		action("actions/setup-go", "actions/setup-go", "0aaccfd150d50ccaeb58ebd88d36e91967a5f35b"),
		// The local action and the docker:// step are skipped, and the
		// second checkout is a repeat of the first.
		action("example-org/example-action/sub", "example-org/example-action", "main"),
	}}
	got, err := newExtractor(true).Parse(".github/workflows/ci.yml", []byte(workflowSample))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	checkParsed(t, ".github/workflows/ci.yml", got, want)
}

func TestIsLocalSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"https://pypi.example.com/simple", false},
		{"https://github.com/example-org/example-git.git", false},
		{"../example-lib", true},
		{"dist/example_wheel-0.2.0.tar.gz", true},
		{"/srv/wheels/example.whl", true},
		{"file:///srv/wheels/example.whl", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := isLocalSource(tt.in); got != tt.want {
				t.Errorf("isLocalSource(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestExactPin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"==2.31.0", "2.31.0"},
		{"== 3.0", "3.0"},
		{">=6", ""},
		{"", ""},
		{"===1.0", ""},
		{"==1.*", ""},
		{"==1.0,!=1.0.1", ""},
		{"~=1.4", ""},
		{"==", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := exactPin(tt.in); got != tt.want {
				t.Errorf("exactPin(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// debugExtractor returns an Extractor whose debug-level log lines are written
// to the returned buffer.
func debugExtractor() (*Extractor, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return New(Options{DeriveToolchainFromImages: true}, logger), &buf
}

// checkSkipLogged fails the test unless log has a line with msg and no
// occurrence of SECRET.
func checkSkipLogged(t *testing.T, log, msg string) {
	t.Helper()
	if !strings.Contains(log, msg) {
		t.Errorf("log = %q, want a %q line", log, msg)
	}
	if strings.Contains(log, "SECRET") {
		t.Errorf("log = %q, want no SECRET", log)
	}
}

func TestParse_SkippedDockerActionLogRedacted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		uses string
	}{
		// The library splits this at the first ':' after the scheme, so the
		// password is already absent from the name it returns.
		{"userinfo", "docker://user:SECRET@registry.example/img:1"},
		// A token as the userinfo reaches the logged name intact.
		{"token userinfo", "docker://ghp_SECRET0123456789abcdefghij@registry.example/img:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			workflow := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n" +
				"      - uses: actions/checkout@v4\n" +
				"      - uses: " + tt.uses + "\n"
			e, buf := debugExtractor()
			got, err := e.Parse(".github/workflows/ci.yml", []byte(workflow))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			want := model.ParsedManifest{Ecosystem: "github-actions", Kind: model.ManifestKindManifest, Dependencies: []model.Dependency{{
				Package:     model.Package{PURL: "pkg:githubactions/actions/checkout", Ecosystem: "github-actions", Name: "actions/checkout"},
				Version:     "v4",
				Requirement: "v4",
				Direct:      model.DirectYes,
				Scope:       "runtime",
			}}}
			checkParsed(t, ".github/workflows/ci.yml", got, want)
			checkSkipLogged(t, buf.String(), "docker action skipped")
		})
	}
}

func TestParse_SkippedLocalPackageLogRedacted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pkgName string
	}{
		// Only the package name is logged, not the source URL holding the
		// credential.
		{"userinfo in source url", "example-wheel"},
		// A token-shaped package name reaches the log line intact.
		{"token package name", "ghp_SECRET0123456789abcdefghij"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lock := `[[package]]
name = "click"
version = "8.1.7"
description = "Composable command line interface toolkit"
optional = false
python-versions = ">=3.7"
files = []

[[package]]
name = "` + tt.pkgName + `"
version = "0.2.0"
description = "A local archive"
optional = false
python-versions = "*"
files = []

[package.source]
type = "file"
url = "file://user:SECRET@fileserver.example/wheels/example_wheel-0.2.0.tar.gz"

[metadata]
lock-version = "2.0"
python-versions = "^3.11"
content-hash = "0000000000000000000000000000000000000000000000000000000000000000"
`
			e, buf := debugExtractor()
			got, err := e.Parse("poetry.lock", []byte(lock))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			want := model.ParsedManifest{Ecosystem: "pypi", Kind: model.ManifestKindLockfile, Dependencies: []model.Dependency{
				pypiDep("click", "click", "8.1.7", "8.1.7", model.DirectUnknown, "runtime"),
			}}
			checkParsed(t, "poetry.lock", got, want)
			checkSkipLogged(t, buf.String(), "local package skipped")
		})
	}
}
