package extract

import (
	"maps"
	"slices"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const uvLockSources = `version = 1
revision = 2
requires-python = ">=3.11"

[[package]]
name = "example-root"
version = "0.1.0"
source = { editable = "." }
dependencies = [
    { name = "example-member" },
]

[package.metadata]
requires-dist = [{ name = "example-member", editable = "packages/member" }]

[[package]]
name = "example-workspace"
version = "0.0.0"
source = { virtual = "." }

[[package]]
name = "example-member"
version = "0.2.0"
source = { directory = "packages/member" }

[[package]]
name = "example-wheel"
version = "0.3.0"
source = { path = "dist/example_wheel-0.3.0-py3-none-any.whl" }

[[package]]
name = "example-git"
version = "1.0.0"
source = { git = "https://github.com/example-org/example-git?rev=v1.0.0#0000000000000000000000000000000000000004" }

[[package]]
name = "requests"
version = "2.32.5"
source = { registry = "https://pypi.org/simple" }
`

func TestParse_UVLockLocalSources(t *testing.T) {
	t.Parallel()
	got, err := newExtractor(true).Parse("uv.lock", []byte(uvLockSources))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	checkParsed(t, "uv.lock", got, model.ParsedManifest{Ecosystem: "pypi", Kind: model.ManifestKindLockfile, Dependencies: []model.Dependency{
		pypiDep("example-git", "example-git", "1.0.0", "1.0.0", model.DirectUnknown, "runtime"),
		pypiDep("requests", "requests", "2.32.5", "2.32.5", model.DirectUnknown, "runtime"),
	}})
}

// TestParse_UVLockTyper parses the uv.lock of github.com/fastapi/typer
// (MIT licence), whose only local package is typer itself.
func TestParse_UVLockTyper(t *testing.T) {
	t.Parallel()
	got, err := newExtractor(true).Parse("uv.lock", readTestdata(t, "typer-uv.lock"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	byName := make(map[string]model.Dependency, len(got.Dependencies))
	for _, d := range got.Dependencies {
		byName[d.Package.Name] = d
	}
	if d, ok := byName["typer"]; ok {
		t.Errorf("Parse() kept the local package typer: %+v", d)
	}
	for name, version := range map[string]string{
		"click":           "8.3.1",
		"rich":            "15.0.0",
		"shellingham":     "1.5.4",
		"annotated-types": "0.7.0",
	} {
		d, ok := byName[name]
		if !ok {
			t.Errorf("Parse() dropped the registry package %s", name)
			continue
		}
		if d.Version != version || d.Package.PURL != "pkg:pypi/"+name || d.Direct != model.DirectUnknown {
			t.Errorf("Parse() %s = %+v, want version %s, PURL pkg:pypi/%s, DirectUnknown", name, d, version, name)
		}
	}
	// The file has 85 [[package]] tables with distinct names; only typer
	// is local.
	if len(got.Dependencies) != 84 {
		t.Errorf("Parse() returned %d dependencies, want 84", len(got.Dependencies))
	}
}

func TestUVLocalPackages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "every source kind",
			content: uvLockSources,
			want:    []string{"example-member", "example-root", "example-wheel", "example-workspace"},
		},
		{
			name:    "CRLF line endings",
			content: "[[package]]\r\nname = \"app\"\r\nsource = { editable = \".\" }\r\n",
			want:    []string{"app"},
		},
		{
			name:    "unreadable source line stays",
			content: "[[package]]\nname = \"app\"\nsource = { \n  editable = \".\" }\n",
			want:    nil,
		},
		{
			name:    "unreadable name stays",
			content: "[[package]]\nname = 'app'\nsource = { editable = \".\" }\n",
			want:    nil,
		},
		{
			name:    "keys under a sub-table are not the package's",
			content: "[[package]]\nname = \"lib\"\nsource = { registry = \"https://pypi.org/simple\" }\n\n[package.metadata]\nname = \"other\"\nsource = { editable = \".\" }\n",
			want:    nil,
		},
		{
			name:    "indented keys inside arrays are ignored",
			content: "[[package]]\nname = \"lib\"\nsource = { registry = \"https://pypi.org/simple\" }\ndependencies = [\n    { name = \"x\", source = { editable = \".\" } },\n]\n",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := slices.Sorted(maps.Keys(uvLocalPackages([]byte(tt.content))))
			if !slices.Equal(got, tt.want) {
				t.Errorf("uvLocalPackages() = %q, want %q", got, tt.want)
			}
		})
	}
}
