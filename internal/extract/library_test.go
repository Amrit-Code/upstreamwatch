package extract

import (
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
