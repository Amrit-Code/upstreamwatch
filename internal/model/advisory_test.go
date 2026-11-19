package model

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestAdvisory_CVEs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		id      string
		aliases []string
		want    []string
	}{
		{name: "CVE id alone", id: "CVE-2026-0001", want: []string{"CVE-2026-0001"}},
		{name: "CVE id first, then CVE aliases in order", id: "CVE-2026-0001",
			aliases: []string{"GHSA-aaaa-bbbb-cccc", "CVE-2026-0003", "CVE-2026-0002"},
			want:    []string{"CVE-2026-0001", "CVE-2026-0003", "CVE-2026-0002"}},
		{name: "non-CVE id contributes nothing", id: "GHSA-aaaa-bbbb-cccc",
			aliases: []string{"CVE-2026-0002", "PYSEC-2026-1"},
			want:    []string{"CVE-2026-0002"}},
		{name: "repeats dropped, first position kept", id: "GO-2026-0001",
			aliases: []string{"CVE-2026-0002", "GHSA-aaaa-bbbb-cccc", "CVE-2026-0001", "CVE-2026-0002"},
			want:    []string{"CVE-2026-0002", "CVE-2026-0001"}},
		{name: "alias repeating the CVE id", id: "CVE-2026-0001",
			aliases: []string{"CVE-2026-0001"},
			want:    []string{"CVE-2026-0001"}},
		{name: "no CVE anywhere", id: "GO-2026-0001", aliases: []string{"GHSA-aaaa-bbbb-cccc"}, want: nil},
		{name: "no aliases, non-CVE id", id: "RUSTSEC-2026-0001", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := Advisory{ID: tt.id, Aliases: slices.Clone(tt.aliases)}
			got := a.CVEs()
			if len(got) != len(tt.want) || (len(tt.want) > 0 && !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("Advisory{ID: %q, Aliases: %q}.CVEs() = %q, want %q", tt.id, tt.aliases, got, tt.want)
			}
			if !slices.Equal(a.Aliases, tt.aliases) {
				t.Errorf("CVEs() changed Aliases to %q, want %q", a.Aliases, tt.aliases)
			}
		})
	}
}

func TestSourceDB(t *testing.T) {
	t.Parallel()
	tests := []struct{ id, want string }{
		{"GO-2026-4599", "GO"},
		{"GHSA-aaaa-bbbb-cccc", "GHSA"},
		{"PYSEC-2026-2275", "PYSEC"},
		{"RUSTSEC-2026-0001", "RUSTSEC"},
		{"CVE-2026-0001", "CVE"},
		{"OSV-2026-1", "OSV"},
		{"A-", "A"},
		{"nodash", "nodash"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := SourceDB(tt.id); got != tt.want {
			t.Errorf("SourceDB(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestPURLType(t *testing.T) {
	t.Parallel()
	tests := []struct{ purl, want string }{
		{"pkg:golang/github.com/example-org/lib", "golang"},
		{"pkg:golang/toolchain", "golang"},
		{"pkg:githubactions/actions/checkout", "githubactions"},
		{"pkg:npm/%40scope/name", "npm"},
		{"pkg:docker/library/golang", "docker"},
		{"pkg:maven/org.example/example-lib", "maven"},
		{"pkg:golang", ""},
		{"golang/github.com/example-org/lib", ""},
		{"https://github.com/example-org/lib", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := PURLType(tt.purl); got != tt.want {
			t.Errorf("PURLType(%q) = %q, want %q", tt.purl, got, tt.want)
		}
	}
}

func TestOSVEcosystem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		purl   string
		want   string
		wantOK bool
	}{
		{"pkg:golang/github.com/example-org/lib", "Go", true},
		{"pkg:golang/toolchain", "Go", true},
		{"pkg:pypi/example-lib", "PyPI", true},
		{"pkg:npm/%40scope/name", "npm", true},
		{"pkg:cargo/example-crate", "crates.io", true},
		{"pkg:gem/example-gem", "RubyGems", true},
		{"pkg:nuget/Example.Lib", "NuGet", true},
		{"pkg:maven/org.example/example-lib", "Maven", true},
		{"pkg:composer/example-org/lib", "Packagist", true},
		{"pkg:pub/example_lib", "Pub", true},
		{"pkg:hex/example_lib", "Hex", true},
		{"pkg:githubactions/actions/checkout", "GitHub Actions", true},
		{"pkg:docker/library/golang", "", false},
		{"pkg:generic/example", "", false},
		{"pkg:upm/com.example.lib", "", false},
		{"not-a-purl", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := OSVEcosystem(tt.purl)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("OSVEcosystem(%q) = (%q, %v), want (%q, %v)", tt.purl, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestAdvisoryMatch_IsOpen(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		m    AdvisoryMatch
		want bool
	}{
		{"no end is open", AdvisoryMatch{ExposedFrom: from}, true},
		{"zero match is open", AdvisoryMatch{}, true},
		{"end set is closed", AdvisoryMatch{ExposedFrom: from, ExposedTo: from.Add(time.Hour)}, false},
		{"end equal to start is closed", AdvisoryMatch{ExposedFrom: from, ExposedTo: from}, false},
	}
	for _, tt := range tests {
		if got := tt.m.IsOpen(); got != tt.want {
			t.Errorf("%s: IsOpen() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestAdvisory_IsWithdrawn(t *testing.T) {
	t.Parallel()
	if (Advisory{ID: "GO-2026-0001"}).IsWithdrawn() {
		t.Error("IsWithdrawn() = true for an advisory with no withdrawn time, want false")
	}
	if !(Advisory{ID: "GO-2026-0001", Withdrawn: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}).IsWithdrawn() {
		t.Error("IsWithdrawn() = false for a withdrawn advisory, want true")
	}
}
