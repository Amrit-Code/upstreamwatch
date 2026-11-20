package timeline

import (
	"slices"
	"testing"
)

func TestSettingsFingerprint(t *testing.T) {
	t.Parallel()
	base := TargetSettings{
		Slug:         "alpha",
		Ecosystems:   []string{"pypi", "golang"},
		ExcludePaths: []string{"examples", "docs/site"},
	}

	t.Run("canonical text", func(t *testing.T) {
		t.Parallel()
		want := `v2;unity=false;eco="golang","pypi";exclude="docs/site","examples"`
		if got := settingsFingerprint(base); got != want {
			t.Errorf("settingsFingerprint(%+v) = %q, want %q", base, got, want)
		}
		if got, want := settingsFingerprint(TargetSettings{}), "v2;unity=false;eco=;exclude="; got != want {
			t.Errorf("settingsFingerprint(zero) = %q, want %q", got, want)
		}
	})

	equal := []struct {
		name string
		in   TargetSettings
	}{
		{"slug does not matter", TargetSettings{Slug: "beta", Ecosystems: base.Ecosystems, ExcludePaths: base.ExcludePaths}},
		{"order of ecosystems", TargetSettings{Ecosystems: []string{"golang", "pypi"}, ExcludePaths: base.ExcludePaths}},
		{"duplicate ecosystems", TargetSettings{Ecosystems: []string{"pypi", "golang", "pypi"}, ExcludePaths: base.ExcludePaths}},
		{"order of exclusions", TargetSettings{Ecosystems: base.Ecosystems, ExcludePaths: []string{"docs/site", "examples"}}},
		{"duplicate exclusions", TargetSettings{Ecosystems: base.Ecosystems, ExcludePaths: []string{"examples", "docs/site", "examples"}}},
		{"trailing slash", TargetSettings{Ecosystems: base.Ecosystems, ExcludePaths: []string{"examples/", "docs/site//"}}},
		{"with and without slash", TargetSettings{Ecosystems: base.Ecosystems, ExcludePaths: []string{"examples", "examples/", "docs/site"}}},
	}
	want := settingsFingerprint(base)
	for _, tt := range equal {
		t.Run("equal/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := settingsFingerprint(tt.in); got != want {
				t.Errorf("settingsFingerprint(%+v) = %q, want %q", tt.in, got, want)
			}
		})
	}

	different := []struct {
		name string
		in   TargetSettings
	}{
		{"unity", TargetSettings{Unity: true, Ecosystems: base.Ecosystems, ExcludePaths: base.ExcludePaths}},
		{"ecosystem added", TargetSettings{Ecosystems: []string{"pypi", "golang", "npm"}, ExcludePaths: base.ExcludePaths}},
		{"ecosystem removed", TargetSettings{Ecosystems: []string{"golang"}, ExcludePaths: base.ExcludePaths}},
		{"no allowlist", TargetSettings{ExcludePaths: base.ExcludePaths}},
		{"exclusion added", TargetSettings{Ecosystems: base.Ecosystems, ExcludePaths: []string{"examples", "docs/site", "tools"}}},
		{"exclusion removed", TargetSettings{Ecosystems: base.Ecosystems, ExcludePaths: []string{"examples"}}},
		{"exclusion renamed", TargetSettings{Ecosystems: base.Ecosystems, ExcludePaths: []string{"examples2", "docs/site"}}},
		{"no exclusions", TargetSettings{Ecosystems: base.Ecosystems}},
	}
	for _, tt := range different {
		t.Run("different/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := settingsFingerprint(tt.in); got == want {
				t.Errorf("settingsFingerprint(%+v) = %q, want it to differ from %+v", tt.in, got, base)
			}
		})
	}

	t.Run("lists that join alike stay distinct", func(t *testing.T) {
		t.Parallel()
		pairs := [][2]TargetSettings{
			{{Ecosystems: nil}, {Ecosystems: []string{""}}},
			{{Ecosystems: []string{"a,b"}}, {Ecosystems: []string{"a", "b"}}},
			{{ExcludePaths: []string{`x";exclude="y`}}, {ExcludePaths: []string{"x", "y"}}},
			{{Ecosystems: []string{"golang"}}, {ExcludePaths: []string{"golang"}}},
		}
		for _, p := range pairs {
			if a, b := settingsFingerprint(p[0]), settingsFingerprint(p[1]); a == b {
				t.Errorf("settingsFingerprint(%+v) = settingsFingerprint(%+v) = %q, want them distinct", p[0], p[1], a)
			}
		}
	})

	t.Run("input not modified", func(t *testing.T) {
		t.Parallel()
		in := TargetSettings{Ecosystems: []string{"pypi", "golang", "pypi"}, ExcludePaths: []string{"b/", "a"}}
		eco, ex := slices.Clone(in.Ecosystems), slices.Clone(in.ExcludePaths)
		settingsFingerprint(in)
		if !slices.Equal(in.Ecosystems, eco) || !slices.Equal(in.ExcludePaths, ex) {
			t.Errorf("settingsFingerprint modified its input: %+v", in)
		}
	})
}
