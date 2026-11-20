package timeline

import (
	"slices"
	"strconv"
	"strings"
)

// extractorVersion is part of every settings fingerprint, so a change to it
// rebuilds every repo's timeline from the root. Bump when any extractor's
// output for the same bytes changes.
const extractorVersion = 2

// settingsFingerprint returns the canonical text of the settings t's timeline
// is built with, for example
// `v1;unity=false;eco="golang","pypi";exclude="examples"`. Ecosystems and
// exclusions are sorted and de-duplicated, and exclusions lose any trailing
// slash, so settings that index the same files give the same string. Each
// list element is Go-quoted, so an empty element or one holding a comma or
// semicolon cannot make two different settings read alike.
func settingsFingerprint(t TargetSettings) string {
	excludes := make([]string, len(t.ExcludePaths))
	for i, p := range t.ExcludePaths {
		excludes[i] = strings.TrimRight(p, "/")
	}
	return "v" + strconv.Itoa(extractorVersion) +
		";unity=" + strconv.FormatBool(t.Unity) +
		";eco=" + canonicalList(t.Ecosystems) +
		";exclude=" + canonicalList(excludes)
}

// canonicalList returns items sorted, de-duplicated, each Go-quoted, and
// joined with commas. The input slice is not modified.
func canonicalList(items []string) string {
	sorted := slices.Compact(slices.Sorted(slices.Values(items)))
	for i, s := range sorted {
		sorted[i] = strconv.Quote(s)
	}
	return strings.Join(sorted, ",")
}
