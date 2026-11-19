package vuln

import "testing"

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		purl      string
		version   string
		want      string
		wantExact bool
	}{
		{"go module strips v", "pkg:golang/github.com/example-org/lib", "v1.2.3", "1.2.3", true},
		{"go module without v", "pkg:golang/github.com/example-org/lib", "1.2.3", "1.2.3", true},
		{"go module strips one v only", "pkg:golang/github.com/example-org/lib", "vv1.0.0", "v1.0.0", true},
		{"go pseudo-version", "pkg:golang/github.com/example-org/lib", "v0.0.0-20260101000000-abcdef123456", "0.0.0-20260101000000-abcdef123456", true},
		{"toolchain go prefix", toolchainPURL, "go1.26.3", "1.26.3", true},
		{"toolchain bare", toolchainPURL, "1.26.3", "1.26.3", true},
		{"toolchain two components", toolchainPURL, "1.26", "1.26.0", false},
		{"toolchain go prefix two components", toolchainPURL, "go1.26", "1.26.0", false},
		{"toolchain release candidate kept", toolchainPURL, "1.26rc1", "1.26rc1", true},
		{"pypi unchanged", "pkg:pypi/example-lib", "1.0rc1", "1.0rc1", true},
		{"npm keeps v", "pkg:npm/example-lib", "v1.2.3", "v1.2.3", true},
		{"actions major tag floats", "pkg:githubactions/example-org/action", "v4", "4.0.0", false},
		{"actions minor tag floats", "pkg:githubactions/example-org/action", "v4.1", "4.1.0", false},
		{"actions full tag exact", "pkg:githubactions/example-org/action", "v4.1.2", "4.1.2", true},
		{"actions tag without v floats", "pkg:githubactions/example-org/action", "4", "4.0.0", false},
		{"actions commit SHA exact", "pkg:githubactions/example-org/action", "0123456789abcdef0123456789abcdef01234567", "0123456789abcdef0123456789abcdef01234567", true},
		{"actions branch exact", "pkg:githubactions/example-org/action", "main", "main", true},
		{"empty", "pkg:golang/github.com/example-org/lib", "", "", false},
		{"empty toolchain", toolchainPURL, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, gotExact := normalizeVersion(tt.purl, tt.version)
			if got != tt.want || gotExact != tt.wantExact {
				t.Errorf("normalizeVersion(%q, %q) = (%q, %v), want (%q, %v)", tt.purl, tt.version, got, gotExact, tt.want, tt.wantExact)
			}
		})
	}
}

func TestNormalizeVersion_SchemeComparisons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		purl string
		a, b string
		want int
	}{
		{"go patch", "pkg:golang/github.com/example-org/lib", "1.2.2", "1.2.3", -1},
		{"go prerelease below release", toolchainPURL, "1.26.0-0", "1.26.0", -1},
		{"pypi rc below release", "pkg:pypi/example-lib", "1.0rc1", "1.0", -1},
		{"pypi post above release", "pkg:pypi/example-lib", "2.0.post1", "2.0", 1},
		{"actions v stripped", "pkg:githubactions/example-org/action", "v4.1.0", "4.1.0", 0},
		{"npm minor", "pkg:npm/example-lib", "1.10.0", "1.9.0", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, ok := schemeFor(tt.purl)
			if !ok {
				t.Fatalf("schemeFor(%q) found no scheme", tt.purl)
			}
			if got := s.compare(tt.a, tt.b); got != tt.want {
				t.Errorf("compare(%q, %q) under %s = %d, want %d", tt.a, tt.b, s.name, got, tt.want)
			}
		})
	}
}

func TestSchemeFor_UnknownType(t *testing.T) {
	t.Parallel()
	for _, purl := range []string{"pkg:docker/library/golang", "pkg:generic/example", "not-a-purl"} {
		if s, ok := schemeFor(purl); ok {
			t.Errorf("schemeFor(%q) = %q, want no scheme", purl, s.name)
		}
	}
}
