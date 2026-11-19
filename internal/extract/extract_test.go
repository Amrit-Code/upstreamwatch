package extract

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/git-pkgs/manifests"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// newExtractor returns an Extractor with toolchain derivation set as given
// and a logger that discards everything.
func newExtractor(derive bool) *Extractor {
	return New(Options{DeriveToolchainFromImages: derive}, slog.New(slog.DiscardHandler))
}

// readTestdata returns the content of a file under testdata.
func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata %s: %v", name, err)
	}
	return b
}

// checkParsed compares a parse result with the expected one, printing each
// dependency on its own line when they differ.
func checkParsed(t *testing.T, p string, got, want model.ParsedManifest) {
	t.Helper()
	if got.Ecosystem != want.Ecosystem || got.Kind != want.Kind {
		t.Errorf("Parse(%q) ecosystem, kind = %q, %q, want %q, %q", p, got.Ecosystem, got.Kind, want.Ecosystem, want.Kind)
	}
	if !reflect.DeepEqual(got.Dependencies, want.Dependencies) {
		t.Errorf("Parse(%q) dependencies:\n got:\n%s want:\n%s", p, formatDeps(got.Dependencies), formatDeps(want.Dependencies))
	}
}

func formatDeps(deps []model.Dependency) string {
	var b strings.Builder
	for _, d := range deps {
		fmt.Fprintf(&b, "  %+v\n", d)
	}
	if len(deps) == 0 {
		b.WriteString("  (none)\n")
	}
	return b.String()
}

func TestPatterns(t *testing.T) {
	t.Parallel()
	want := []string{
		"**/go.mod",
		"**/requirements*.txt",
		"**/pyproject.toml",
		"**/uv.lock",
		"**/poetry.lock",
		"**/Dockerfile",
		"**/Dockerfile.*",
		"**/*.Dockerfile",
		"**/docker-compose*.yml",
		"**/docker-compose*.yaml",
		"**/compose*.yml",
		"**/compose*.yaml",
		".github/workflows/*.yml",
		".github/workflows/*.yaml",
	}
	e := newExtractor(true)
	got := e.Patterns()
	if !slices.Equal(got, want) {
		t.Fatalf("Patterns() = %q, want %q", got, want)
	}
	// The returned slice is the caller's: changing it must not change the
	// next result.
	got[0] = "changed"
	if again := e.Patterns(); again[0] != want[0] {
		t.Errorf("Patterns()[0] after caller change = %q, want %q", again[0], want[0])
	}
}

func TestFormatOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want fileFormat
	}{
		{"go.mod", formatGoMod},
		{"tools/go.mod", formatGoMod},
		{"requirements.txt", formatPythonManifest},
		{"svc/requirements-dev.txt", formatPythonManifest},
		{"pyproject.toml", formatPythonManifest},
		{"uv.lock", formatPythonLockfile},
		{"py/poetry.lock", formatPythonLockfile},
		{"Dockerfile", formatDockerfile},
		{"build/Dockerfile.prod", formatDockerfile},
		{"app.Dockerfile", formatDockerfile},
		{"docker-compose.yml", formatCompose},
		{"deploy/docker-compose.override.yaml", formatCompose},
		{"compose.yaml", formatCompose},
		{"compose.dev.yml", formatCompose},
		{".github/workflows/ci.yml", formatActions},
		{".github/workflows/release.yaml", formatActions},
		{"sub/.github/workflows/ci.yml", formatUnsupported},
		{".github/workflows/nested/ci.yml", formatUnsupported},
		{"go.sum", formatUnsupported},
		{"dockerfile", formatUnsupported},
		{"requirements.in", formatUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := formatOf(tt.path); got != tt.want {
				t.Errorf("formatOf(%q) = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}

func TestParse_Unsupported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    string
		content string
	}{
		{"go.sum", "github.com/example-org/lib v1.0.0 h1:AAAA=\n"},
		{"package.json", `{"dependencies":{"left-pad":"1.3.0"}}`},
		{"Packages/manifest.json", `{"dependencies":{"com.unity.ugui":"2.0.0"}}`},
		{"sub/.github/workflows/ci.yml", "jobs: {}\n"},
		{"README.md", "# readme\n"},
	}
	e := newExtractor(true)
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			_, err := e.Parse(tt.path, []byte(tt.content))
			if !errors.Is(err, model.ErrUnsupportedFile) {
				t.Errorf("Parse(%q) error = %v, want one wrapping model.ErrUnsupportedFile", tt.path, err)
			}
		})
	}
}

func TestParse_InvalidContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    string
		content string
	}{
		{"go.mod", "module example.com/app\n\nrequire github.com/example-org/lib not-a-version\n"},
		{"Dockerfile", "FROM\n"},
		{"Dockerfile.two", "FROM alpine extra words\n"},
		{"Dockerfile.ref", "FROM golang:\n"},
		{"Dockerfile.heredoc", "FROM alpine\nRUN <<EOF\nfrom os import path\nEOF\nFROM alpine extra words\n"},
		{"docker-compose.yml", "services: [a\n"},
		{"compose.yaml", "services:\n  - web\n"},
		{"compose.yml", "services:\n  web:\n    image:\n      name: x\n"},
		{"compose.dev.yml", "services:\n  web:\n    image: \"two words\"\n"},
		{"pyproject.toml", "[[[ not toml\n"},
		{"uv.lock", "[[[ not toml\n"},
		{".github/workflows/ci.yml", ": : bad\n\t- x"},
	}
	e := newExtractor(true)
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			_, err := e.Parse(tt.path, []byte(tt.content))
			if err == nil {
				t.Fatalf("Parse(%q) error = nil, want a parse error", tt.path)
			}
			if errors.Is(err, model.ErrUnsupportedFile) {
				t.Errorf("Parse(%q) error = %v, want one not wrapping model.ErrUnsupportedFile", tt.path, err)
			}
			if !strings.Contains(err.Error(), tt.path) {
				t.Errorf("Parse(%q) error = %q, want it to name the path", tt.path, err)
			}
		})
	}
}

func TestParse_PURLVersionless(t *testing.T) {
	t.Parallel()
	samples := []struct {
		path    string
		content []byte
	}{
		{"go.mod", []byte(goModSample)},
		{"Dockerfile", []byte(dockerfileSample)},
		{"docker-compose.yml", []byte(composeSample)},
		{"requirements.txt", []byte(requirementsSample)},
		{"pyproject.toml", []byte(pyprojectSample)},
		{"uv.lock", readTestdata(t, "uv.lock")},
		{"poetry.lock", readTestdata(t, "poetry.lock")},
		{".github/workflows/ci.yml", []byte(workflowSample)},
	}
	e := newExtractor(true)
	for _, s := range samples {
		t.Run(s.path, func(t *testing.T) {
			t.Parallel()
			pm, err := e.Parse(s.path, s.content)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", s.path, err)
			}
			if len(pm.Dependencies) == 0 {
				t.Fatalf("Parse(%q) returned no dependencies", s.path)
			}
			for _, d := range pm.Dependencies {
				if strings.ContainsAny(d.Package.PURL, "@?#") || !strings.HasPrefix(d.Package.PURL, "pkg:") {
					t.Errorf("Parse(%q) PURL %q, want a version-less package URL", s.path, d.Package.PURL)
				}
			}
		})
	}
}

func TestVersionless(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"pkg:pypi/requests@2.31.0", "pkg:pypi/requests"},
		{"pkg:docker/golang", "pkg:docker/golang"},
		{"pkg:docker/registry.example.com:5000/team/app", "pkg:docker/registry.example.com:5000/team/app"},
		{"pkg:githubactions/actions/checkout@v4?repository_url=x#sub/dir", "pkg:githubactions/actions/checkout"},
		{"pkg:npm/%40scope/name@1.0.0", "pkg:npm/%40scope/name"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := versionless(tt.in); got != tt.want {
				t.Errorf("versionless(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestEcosystemNamesMatchLibrary checks that the ecosystem names used by the
// parsers written here are the library's own spelling.
func TestEcosystemNamesMatchLibrary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file string
		want string
	}{
		{"go.mod", ecosystemGolang},
		{"Dockerfile", ecosystemDocker},
		{"docker-compose.yml", ecosystemDocker},
		{"requirements.txt", ecosystemPyPI},
		{".github/workflows/ci.yml", ecosystemActions},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()
			got, _, ok := manifests.Identify(tt.file)
			if !ok || got != tt.want {
				t.Errorf("manifests.Identify(%q) = %q, %v, want %q, true", tt.file, got, ok, tt.want)
			}
		})
	}
}
