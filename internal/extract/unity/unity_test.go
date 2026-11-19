package unity_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/extract/unity"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
)

var _ timeline.Extractor = (*unity.Extractor)(nil)

var editor = model.Package{PURL: "pkg:generic/unity-editor", Ecosystem: "generic", Name: "unity-editor"}

func upm(name string) model.Package {
	return model.Package{PURL: "pkg:upm/" + name, Ecosystem: "upm", Name: name}
}

func depth(d int) *int { return &d }

func TestPatterns(t *testing.T) {
	t.Parallel()
	got := unity.New().Patterns()
	want := []string{
		"**/ProjectSettings/ProjectVersion.txt",
		"**/Packages/manifest.json",
		"**/Packages/packages-lock.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Patterns() = %q, want %q", got, want)
	}
}

func TestParse_ProjectVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		content string
		want    []model.Dependency
	}{
		{
			name:    "with revision",
			path:    "ProjectSettings/ProjectVersion.txt",
			content: "m_EditorVersion: 6000.0.23f1\nm_EditorVersionWithRevision: 6000.0.23f1 (1c4764c07fb4)\n",
			want: []model.Dependency{{
				Package: editor, Version: "6000.0.23f1", Requirement: "6000.0.23f1 (1c4764c07fb4)",
				Direct: model.DirectYes, Scope: "build",
			}},
		},
		{
			name:    "without revision, nested project, CRLF",
			path:    "game/ProjectSettings/ProjectVersion.txt",
			content: "m_EditorVersion: 2019.4.40f1\r\n",
			want: []model.Dependency{{
				Package: editor, Version: "2019.4.40f1", Requirement: "2019.4.40f1",
				Direct: model.DirectYes, Scope: "build",
			}},
		},
		{
			name:    "revision line before version line",
			path:    "ProjectSettings/ProjectVersion.txt",
			content: "m_EditorVersionWithRevision: 2022.3.1f1 (abc)\nm_EditorVersion: 2022.3.1f1",
			want: []model.Dependency{{
				Package: editor, Version: "2022.3.1f1", Requirement: "2022.3.1f1 (abc)",
				Direct: model.DirectYes, Scope: "build",
			}},
		},
		{
			name:    "no version line",
			path:    "ProjectSettings/ProjectVersion.txt",
			content: "m_EditorVersionWithRevision: 6000.0.23f1 (1c4764c07fb4)\nsomething: else\n",
			want:    nil,
		},
		{
			name:    "empty file",
			path:    "ProjectSettings/ProjectVersion.txt",
			content: "",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := unity.New().Parse(tt.path, []byte(tt.content))
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.path, err)
			}
			want := model.ParsedManifest{Ecosystem: "upm", Kind: model.ManifestKindManifest, Dependencies: tt.want}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.path, got, want)
			}
		})
	}
}

func TestParse_Manifest(t *testing.T) {
	t.Parallel()
	content := `{
  "scopedRegistries": [
    {"name": "example", "url": "https://registry.example.com", "scopes": ["com.example"]}
  ],
  "dependencies": {
    "com.unity.ugui": "1.0.0",
    "com.example.preview": "3.0.6-preview",
    "com.example.withref": "https://github.com/example-org/example-pkg.git#v1.4.0",
    "com.example.noref": "https://github.com/example-org/other-pkg.git",
    "com.example.credentials": "https://user:token@github.com/example-org/private-pkg.git#v2",
    "com.example.ssh": "git@github.com:example-org/ssh-pkg.git#abc123",
    "com.example.gitplus": "git+https://github.com/example-org/plus-pkg#main",
    "com.example.dotgit": "example.com/example-org/bare.git",
    "com.example.local": "file:../pkg",
    "com.example.localgit": "file:../x.git#abc"
  },
  "testables": ["com.unity.ugui"]
}`
	want := model.ParsedManifest{
		Ecosystem: "upm",
		Kind:      model.ManifestKindManifest,
		Dependencies: []model.Dependency{
			{Package: upm("com.example.credentials"), Version: "v2", Requirement: "https://github.com/example-org/private-pkg.git#v2"},
			{Package: upm("com.example.dotgit"), Version: "", Requirement: "example.com/example-org/bare.git"},
			{Package: upm("com.example.gitplus"), Version: "main", Requirement: "git+https://github.com/example-org/plus-pkg#main"},
			{Package: upm("com.example.local"), Version: "", Requirement: "file:../pkg"},
			{Package: upm("com.example.localgit"), Version: "", Requirement: "file:../x.git#abc"},
			{Package: upm("com.example.noref"), Version: "", Requirement: "https://github.com/example-org/other-pkg.git"},
			{Package: upm("com.example.preview"), Version: "3.0.6-preview", Requirement: "3.0.6-preview"},
			{Package: upm("com.example.ssh"), Version: "abc123", Requirement: "git@github.com:example-org/ssh-pkg.git#abc123"},
			{Package: upm("com.example.withref"), Version: "v1.4.0", Requirement: "https://github.com/example-org/example-pkg.git#v1.4.0"},
			{Package: upm("com.unity.ugui"), Version: "1.0.0", Requirement: "1.0.0"},
		},
	}
	for i := range want.Dependencies {
		want.Dependencies[i].Direct = model.DirectYes
		want.Dependencies[i].Scope = "runtime"
	}
	got, err := unity.New().Parse("Packages/manifest.json", []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() =\n%+v\nwant\n%+v", got, want)
	}
	assertNoSecret(t, got)
}

func TestParse_PackagesLock(t *testing.T) {
	t.Parallel()
	content := `{
  "dependencies": {
    "com.unity.test-framework": {
      "version": "1.1.33", "depth": 0, "source": "registry",
      "dependencies": {"com.unity.ext.nunit": "1.0.6"}, "url": "https://packages.unity.com"
    },
    "com.unity.modules.ui": {"version": "1.0.0", "depth": 1, "source": "builtin", "dependencies": {}},
    "com.example.hashed": {
      "version": "https://user:token@github.com/example-org/hashed.git#v1.0.0",
      "depth": 0, "source": "git", "dependencies": {},
      "hash": "0123456789abcdef0123456789abcdef01234567"
    },
    "com.example.unhashed": {
      "version": "https://github.com/example-org/unhashed.git#v2.1.0", "depth": 2, "source": "git", "dependencies": {}
    },
    "com.example.noref": {
      "version": "https://github.com/example-org/noref.git", "depth": 0, "source": "git", "dependencies": {}
    },
    "com.example.embedded": {"version": "file:com.example.embedded", "depth": 0, "source": "embedded", "dependencies": {}},
    "com.example.local": {"version": "file:../local-pkg", "depth": 0, "source": "local", "dependencies": {}},
    "com.example.nodepth": {"version": "2.0.0", "source": "registry", "dependencies": {}}
  }
}`
	want := model.ParsedManifest{
		Ecosystem: "upm",
		Kind:      model.ManifestKindLockfile,
		Dependencies: []model.Dependency{
			{Package: upm("com.example.embedded"), Version: "", Requirement: "file:com.example.embedded", Direct: model.DirectYes},
			{Package: upm("com.example.hashed"), Version: "0123456789abcdef0123456789abcdef01234567", Requirement: "https://github.com/example-org/hashed.git#v1.0.0", Direct: model.DirectYes},
			{Package: upm("com.example.local"), Version: "", Requirement: "file:../local-pkg", Direct: model.DirectYes},
			{Package: upm("com.example.nodepth"), Version: "2.0.0", Requirement: "2.0.0", Direct: model.DirectUnknown},
			{Package: upm("com.example.noref"), Version: "", Requirement: "https://github.com/example-org/noref.git", Direct: model.DirectYes},
			{Package: upm("com.example.unhashed"), Version: "v2.1.0", Requirement: "https://github.com/example-org/unhashed.git#v2.1.0", Direct: model.DirectNo},
			{Package: upm("com.unity.modules.ui"), Version: "1.0.0", Requirement: "1.0.0", Direct: model.DirectNo},
			{Package: upm("com.unity.test-framework"), Version: "1.1.33", Requirement: "1.1.33", Direct: model.DirectYes},
		},
	}
	for i := range want.Dependencies {
		want.Dependencies[i].Scope = "runtime"
	}
	got, err := unity.New().Parse("Packages/packages-lock.json", []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() =\n%+v\nwant\n%+v", got, want)
	}
	assertNoSecret(t, got)
}

func TestParse_PackagesLock_Depth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		depth *int
		want  model.Directness
	}{
		{"absent", nil, model.DirectUnknown},
		{"zero", depth(0), model.DirectYes},
		{"one", depth(1), model.DirectNo},
		{"three", depth(3), model.DirectNo},
		{"negative", depth(-1), model.DirectUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry := `"version": "1.0.0", "source": "registry"`
			if tt.depth != nil {
				entry += `, "depth": ` + strconv.Itoa(*tt.depth)
			}
			content := `{"dependencies": {"com.example.pkg": {` + entry + `}}}`
			got, err := unity.New().Parse("Packages/packages-lock.json", []byte(content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(got.Dependencies) != 1 || got.Dependencies[0].Direct != tt.want {
				t.Errorf("Parse(depth %v) = %+v, want Direct %v", tt.depth, got.Dependencies, tt.want)
			}
		})
	}
}

func TestParse_UserinfoNeverReturned(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"https user and token", "https://user:token@github.com/example-org/x.git#v1", "https://github.com/example-org/x.git#v1"},
		{"https token only", "https://token@github.com/example-org/x.git", "https://github.com/example-org/x.git"},
		{"git+https with password", "git+https://user:secret@example.com/example-org/x.git", "git+https://example.com/example-org/x.git"},
		{"ssh scheme user", "ssh://git@example.com/example-org/x.git", "ssh://example.com/example-org/x.git"},
		{"scp-like git user kept", "git@example.com:example-org/x.git", "git@example.com:example-org/x.git"},
		{"scp-like password removed", "user:secret@example.com:example-org/x.git", "example.com:example-org/x.git"},
		{"at sign in fragment kept", "https://example.com/example-org/x.git#feature@1", "https://example.com/example-org/x.git#feature@1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := `{"dependencies": {"com.example.pkg": "` + tt.value + `"}}`
			got, err := unity.New().Parse("Packages/manifest.json", []byte(content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(got.Dependencies) != 1 {
				t.Fatalf("Parse() returned %d dependencies, want 1", len(got.Dependencies))
			}
			if req := got.Dependencies[0].Requirement; req != tt.want {
				t.Errorf("Parse(%q) Requirement = %q, want %q", tt.value, req, tt.want)
			}
			assertNoSecret(t, got)
		})
	}
}

func TestParse_Unsupported(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"Packages/other.json",
		"go.mod",
		"manifest.json",
		"Assets/Packages/packages-lock.json.bak",
		"Settings/ProjectVersion.txt",
	} {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, err := unity.New().Parse(p, []byte(`{}`))
			if !errors.Is(err, model.ErrUnsupportedFile) {
				t.Errorf("Parse(%q) error = %v, want ErrUnsupportedFile", p, err)
			}
		})
	}
}

func TestParse_Malformed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    string
		content string
	}{
		{"Packages/manifest.json", `{"dependencies": {`},
		{"Packages/manifest.json", `{"dependencies": {"com.example.pkg": 1}}`},
		{"Packages/packages-lock.json", `not json`},
		{"Packages/packages-lock.json", `{"dependencies": {"com.example.pkg": {"depth": "zero"}}}`},
		{"ProjectSettings/ProjectVersion.txt", "m_EditorVersion: " + strings.Repeat("x", 70*1024)},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			_, err := unity.New().Parse(tt.path, []byte(tt.content))
			if err == nil {
				t.Fatalf("Parse(%q) error = nil, want an error", tt.path)
			}
			if errors.Is(err, model.ErrUnsupportedFile) {
				t.Errorf("Parse(%q) error = %v, want not ErrUnsupportedFile", tt.path, err)
			}
			if !strings.Contains(err.Error(), tt.path) {
				t.Errorf("Parse(%q) error = %q, want it to name the path", tt.path, err)
			}
		})
	}
}

// TestParse_RealProject parses the three files of the MIT-licensed public
// project github.com/dbrizov/NaughtyAttributes, copied under testdata.
func TestParse_RealProject(t *testing.T) {
	t.Parallel()
	e := unity.New()
	read := func(p string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("testdata", "naughtyattributes", filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	parse := func(p string) model.ParsedManifest {
		t.Helper()
		pm, err := e.Parse(p, read(p))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", p, err)
		}
		return pm
	}
	find := func(pm model.ParsedManifest, name string) model.Dependency {
		t.Helper()
		for _, d := range pm.Dependencies {
			if d.Package.Name == name {
				return d
			}
		}
		t.Fatalf("package %q not found", name)
		return model.Dependency{}
	}

	pv := parse("ProjectSettings/ProjectVersion.txt")
	wantEditor := model.Dependency{
		Package: editor, Version: "2022.3.62f3", Requirement: "2022.3.62f3 (96770f904ca7)",
		Direct: model.DirectYes, Scope: "build",
	}
	if len(pv.Dependencies) != 1 || pv.Dependencies[0] != wantEditor {
		t.Errorf("ProjectVersion.txt dependencies = %+v, want [%+v]", pv.Dependencies, wantEditor)
	}

	mf := parse("Packages/manifest.json")
	if got, want := len(mf.Dependencies), 34; got != want {
		t.Errorf("manifest.json has %d dependencies, want %d", got, want)
	}
	wantVS := model.Dependency{
		Package: upm("com.unity.ide.visualstudio"), Version: "2.0.27", Requirement: "2.0.27",
		Direct: model.DirectYes, Scope: "runtime",
	}
	if got := find(mf, "com.unity.ide.visualstudio"); got != wantVS {
		t.Errorf("manifest.json visualstudio = %+v, want %+v", got, wantVS)
	}
	if mf.Dependencies[0].Package.Name != "com.unity.2d.sprite" {
		t.Errorf("manifest.json first dependency = %q, want com.unity.2d.sprite", mf.Dependencies[0].Package.Name)
	}

	lock := parse("Packages/packages-lock.json")
	if lock.Kind != model.ManifestKindLockfile || lock.Ecosystem != "upm" {
		t.Errorf("packages-lock.json Kind, Ecosystem = %q, %q, want lockfile, upm", lock.Kind, lock.Ecosystem)
	}
	for _, want := range []model.Dependency{
		{Package: upm("com.unity.test-framework"), Version: "1.1.33", Requirement: "1.1.33", Direct: model.DirectNo, Scope: "runtime"},
		{Package: upm("com.unity.ext.nunit"), Version: "1.0.6", Requirement: "1.0.6", Direct: model.DirectNo, Scope: "runtime"},
		{Package: upm("com.unity.ugui"), Version: "2.5.0", Requirement: "2.5.0", Direct: model.DirectYes, Scope: "runtime"},
	} {
		if got := find(lock, want.Package.Name); got != want {
			t.Errorf("packages-lock.json %s = %+v, want %+v", want.Package.Name, got, want)
		}
	}
}

// assertNoSecret fails when any returned string still carries a credential.
func assertNoSecret(t *testing.T, pm model.ParsedManifest) {
	t.Helper()
	for _, d := range pm.Dependencies {
		for _, s := range []string{d.Version, d.Requirement, d.Package.PURL, d.Package.Name} {
			if strings.Contains(s, "token") || strings.Contains(s, "secret") {
				t.Errorf("dependency %s carries a credential: %q", d.Package.Name, s)
			}
		}
	}
}
