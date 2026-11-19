package timeline

import (
	"reflect"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestUnsafeIdentifier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"github.com/gin-gonic/gin", false},
		{"pkg:golang/github.com/gin-gonic/gin", false},
		{"pkg:npm/%40scope/name", false},
		{"com.unity.textmeshpro", false},
		{"ghcr.io/example-org/image", false},
		{"", false},
		{"pkg @ git+https://example.com/r.git", true},
		{"git+https://example.com/r.git", true},
		{"user@example.com", true},
		{"a\x1b[2Jb", true},
		{"a\x00b", true},
		{"a\x7fb", true},
		{"a\tb", true},
		{"trailing\n", true},
		{"no break", true},
		{"line sep", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := unsafeIdentifier(tt.in); got != tt.want {
				t.Errorf("unsafeIdentifier(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeDeps(t *testing.T) {
	t.Parallel()
	dep := func(purl, name, version, requirement string) model.Dependency {
		return model.Dependency{Package: model.Package{PURL: purl, Ecosystem: "pypi", Name: name}, Version: version, Requirement: requirement}
	}
	tests := []struct {
		name        string
		in          []model.Dependency
		want        []model.Dependency
		wantDropped int
	}{
		{name: "empty", in: nil, want: []model.Dependency{}},
		{
			name: "safe kept unchanged",
			in:   []model.Dependency{dep("pkg:pypi/requests", "requests", "2.31.0", ">=2.31")},
			want: []model.Dependency{dep("pkg:pypi/requests", "requests", "2.31.0", ">=2.31")},
		},
		{
			name:        "unsafe name dropped",
			in:          []model.Dependency{dep("pkg:pypi/x", "x @ https://u:p@example.com/x", "", ""), dep("pkg:pypi/y", "y", "", "")},
			want:        []model.Dependency{dep("pkg:pypi/y", "y", "", "")},
			wantDropped: 1,
		},
		{
			name:        "unsafe purl dropped",
			in:          []model.Dependency{dep("pkg:pypi/x\x1b]0;t\x07", "x", "", "")},
			want:        []model.Dependency{},
			wantDropped: 1,
		},
		{
			name: "version and requirement redacted",
			in:   []model.Dependency{dep("pkg:pypi/z", "z", "https://user:pw@example.com/z", "git+https://x-access-token:tok@example.com/z.git")},
			want: []model.Dependency{dep("pkg:pypi/z", "z", "https://[REDACTED]@example.com/z", "git+https://[REDACTED]@example.com/z.git")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, dropped := sanitizeDeps(tt.in)
			if !reflect.DeepEqual(got, tt.want) || dropped != tt.wantDropped {
				t.Errorf("sanitizeDeps(%+v) = %+v, %d, want %+v, %d", tt.in, got, dropped, tt.want, tt.wantDropped)
			}
		})
	}
}
