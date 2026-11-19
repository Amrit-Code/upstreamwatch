package vuln

import (
	"strings"

	"github.com/git-pkgs/vers"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// toolchainPURL is the package URL Go's stdlib and toolchain advisories map
// to, and the one go directives and golang image tags are recorded under.
const toolchainPURL = "pkg:golang/toolchain"

// normalizeVersion returns the version in the form the advisory database
// uses for purl, and whether it is an exact pin.
func normalizeVersion(purl, version string) (v string, exact bool) {
	if version == "" {
		return "", false
	}
	if model.PURLType(purl) != "golang" {
		return version, true
	}
	v = strings.TrimPrefix(version, "v")
	if purl != toolchainPURL {
		return v, true
	}
	v = strings.TrimPrefix(v, "go")
	if major, minor, ok := strings.Cut(v, "."); ok && isDigits(major) && isDigits(minor) {
		// A go directive or a floating image tag names a minor release
		// line, not a patch release; evaluate it as the line's first.
		return v + ".0", false
	}
	return v, true
}

// isDigits reports whether s is non-empty and only ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// versionScheme is how the versions of one package type are compared.
type versionScheme struct {
	// name is the vers scheme name.
	name string
	// stripV says a leading "v" is dropped from both sides before a
	// comparison (GitHub Actions tags such as v4.1.0).
	stripV bool
}

// schemeFor returns the version scheme for the package type of purl, and
// false when the type has none.
func schemeFor(purl string) (versionScheme, bool) {
	switch model.PURLType(purl) {
	case "golang":
		return versionScheme{name: "go"}, true
	case "pypi":
		return versionScheme{name: "pypi"}, true
	case "npm":
		return versionScheme{name: "npm"}, true
	case "cargo":
		return versionScheme{name: "cargo"}, true
	case "gem":
		return versionScheme{name: "gem"}, true
	case "nuget":
		return versionScheme{name: "nuget"}, true
	case "maven":
		return versionScheme{name: "maven"}, true
	case "composer":
		return versionScheme{name: "composer"}, true
	case "pub":
		return versionScheme{name: "pub"}, true
	case "hex":
		return versionScheme{name: "hex"}, true
	case "githubactions":
		return versionScheme{name: "semver", stripV: true}, true
	}
	return versionScheme{}, false
}

// canonical returns v as the scheme compares it.
func (s versionScheme) canonical(v string) string {
	if s.stripV {
		return strings.TrimPrefix(v, "v")
	}
	return v
}

// valid reports whether the scheme accepts v.
func (s versionScheme) valid(v string) bool {
	return vers.ValidWithScheme(s.canonical(v), s.name)
}

// compare returns -1, 0 or 1 as a is lower than, equal to or higher than b.
func (s versionScheme) compare(a, b string) int {
	return vers.CompareWithScheme(s.canonical(a), s.canonical(b), s.name)
}

// listKey returns v in the form an explicit versions list is matched in:
// normalised for purl, then as the purl's scheme compares it.
func listKey(purl, v string) string {
	n, _ := normalizeVersion(purl, v)
	if s, ok := schemeFor(purl); ok {
		return s.canonical(n)
	}
	return n
}
