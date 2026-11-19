package extract

import (
	"testing"

	"github.com/git-pkgs/manifests"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const goModSample = `module example.com/app

go 1.24.2

toolchain go1.25.1

require (
	github.com/example-org/direct v1.2.0
	github.com/example-org/indirect v0.3.1 // indirect
	github.com/example-org/forked v1.0.0
	github.com/example-org/local v0.1.0
	github.com/example-org/exact v1.0.0
	github.com/example-org/other v1.1.0
)

require github.com/example-org/direct v1.3.0

replace github.com/example-org/forked => github.com/example-fork/forked v1.0.1

replace github.com/example-org/local => ../local

replace github.com/example-org/exact v1.0.0 => github.com/example-fork/exact v1.0.9

replace github.com/example-org/other v1.0.0 => github.com/example-fork/other v1.0.5
`

func goDep(path, version, requirement string, direct model.Directness) model.Dependency {
	return model.Dependency{
		Package:     model.Package{PURL: "pkg:golang/" + path, Ecosystem: "golang", Name: path},
		Version:     version,
		Requirement: requirement,
		Direct:      direct,
		Scope:       "runtime",
	}
}

func toolchainDep(version, requirement string) model.Dependency {
	return model.Dependency{
		Package:     model.Package{PURL: "pkg:golang/toolchain", Ecosystem: "golang", Name: "toolchain"},
		Version:     version,
		Requirement: requirement,
		Direct:      model.DirectYes,
		Scope:       "build",
	}
}

// TestGoPURLMatchesLibrary checks that the PURL built from a require line,
// mixed case included, is the one the manifests library gives the same line.
func TestGoPURLMatchesLibrary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		modPath string
		version string
	}{
		{"github.com/BurntSushi/toml", "v1.6.0"},
		{"github.com/example-org/lib/v2", "v2.0.1"},
		{"gopkg.in/yaml.v3", "v3.0.1"},
	}
	e := newExtractor(true)
	for _, tt := range tests {
		modPath := tt.modPath
		t.Run(modPath, func(t *testing.T) {
			t.Parallel()
			content := []byte("module example.com/app\n\nrequire " + modPath + " " + tt.version + "\n")
			lib, err := manifests.Parse("go.mod", content)
			if err != nil || len(lib.Dependencies) != 1 {
				t.Fatalf("manifests.Parse(require %s) = %+v, %v", modPath, lib, err)
			}
			got, err := e.Parse("go.mod", content)
			if err != nil || len(got.Dependencies) != 1 {
				t.Fatalf("Parse(require %s) = %+v, %v", modPath, got, err)
			}
			if want := "pkg:golang/" + modPath; got.Dependencies[0].Package.PURL != want {
				t.Errorf("Parse(require %s) PURL = %q, want %q", modPath, got.Dependencies[0].Package.PURL, want)
			}
			if got.Dependencies[0].Package.PURL != lib.Dependencies[0].PURL {
				t.Errorf("Parse(require %s) PURL = %q, library gives %q", modPath, got.Dependencies[0].Package.PURL, lib.Dependencies[0].PURL)
			}
		})
	}
}

func TestParse_GoMod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    []model.Dependency
	}{
		{
			name:    "requires, replaces and toolchain",
			content: goModSample,
			want: []model.Dependency{
				goDep("github.com/example-org/direct", "v1.2.0", "v1.2.0", model.DirectYes),
				goDep("github.com/example-org/indirect", "v0.3.1", "v0.3.1", model.DirectNo),
				goDep("github.com/example-fork/forked", "v1.0.1",
					"github.com/example-org/forked => github.com/example-fork/forked v1.0.1", model.DirectYes),
				goDep("github.com/example-org/local", "", "=> ../local", model.DirectYes),
				goDep("github.com/example-fork/exact", "v1.0.9",
					"github.com/example-org/exact => github.com/example-fork/exact v1.0.9", model.DirectYes),
				// The replace names v1.0.0 only, so v1.1.0 is not replaced.
				goDep("github.com/example-org/other", "v1.1.0", "v1.1.0", model.DirectYes),
				toolchainDep("1.25.1", "go 1.24.2"),
			},
		},
		{
			name:    "go directive alone",
			content: "module example.com/app\n\ngo 1.24.2\n",
			want:    []model.Dependency{toolchainDep("1.24.2", "go 1.24.2")},
		},
		{
			name:    "toolchain directive alone",
			content: "module example.com/app\n\ntoolchain go1.25.1\n",
			want:    []model.Dependency{toolchainDep("1.25.1", "toolchain go1.25.1")},
		},
		{
			name:    "toolchain default is ignored",
			content: "module example.com/app\n\ngo 1.22\n\ntoolchain default\n",
			want:    []model.Dependency{toolchainDep("1.22", "go 1.22")},
		},
		{
			// Module paths are case-sensitive, so the PURL keeps the case.
			name:    "module path case is kept",
			content: "module example.com/app\n\nrequire github.com/Example-Org/Lib v1.0.0\n",
			want:    []model.Dependency{goDep("github.com/Example-Org/Lib", "v1.0.0", "v1.0.0", model.DirectYes)},
		},
		{
			name:    "no go or toolchain directive",
			content: "module example.com/app\n\nrequire github.com/example-org/direct v1.2.0\n",
			want:    []model.Dependency{goDep("github.com/example-org/direct", "v1.2.0", "v1.2.0", model.DirectYes)},
		},
		{
			name: "path-wide replace of two modules onto one keeps the first",
			content: "module example.com/app\n\nrequire (\n\tgithub.com/example-org/a v1.0.0\n\tgithub.com/example-org/b v1.0.0\n)\n\n" +
				"replace github.com/example-org/a => github.com/example-fork/ab v1.2.0\n\n" +
				"replace github.com/example-org/b => github.com/example-fork/ab v1.3.0\n",
			want: []model.Dependency{
				goDep("github.com/example-fork/ab", "v1.2.0",
					"github.com/example-org/a => github.com/example-fork/ab v1.2.0", model.DirectYes),
			},
		},
	}
	e := newExtractor(true)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := e.Parse("svc/go.mod", []byte(tt.content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			checkParsed(t, "svc/go.mod", got, model.ParsedManifest{
				Ecosystem:    "golang",
				Kind:         model.ManifestKindManifest,
				Dependencies: tt.want,
			})
		})
	}
}
