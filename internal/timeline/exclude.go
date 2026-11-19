package timeline

import (
	"slices"
	"strings"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// skippedDirs are directory names whose manifests are never indexed, at any
// depth: test fixtures, vendored copies and installed packages.
var skippedDirs = []string{"testdata", "vendor", "node_modules"}

// excludedPath reports whether the slash-separated repository path is not
// indexed: one of its directory segments (not the file name) is in
// skippedDirs, or it equals or lies under one of prefixes. A prefix may end
// in a slash; "examples" and "examples/" both exclude "examples/go.mod" but
// not "examples2/go.mod".
func excludedPath(path string, prefixes []string) bool {
	segments := strings.Split(path, "/")
	for _, dir := range segments[:len(segments)-1] {
		if slices.Contains(skippedDirs, dir) {
			return true
		}
	}
	for _, p := range prefixes {
		p = strings.TrimRight(p, "/")
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// dropFilteredEvents returns the stored events the current rules still
// index: those whose manifest path excludedPath accepts with prefixes and,
// when ecosystems is not empty, whose package ecosystem is listed. It also
// returns the number of events left out. Order is kept and the input slice
// is not modified.
func dropFilteredEvents(events []model.DepEvent, prefixes, ecosystems []string) ([]model.DepEvent, int) {
	var out []model.DepEvent
	dropped := 0
	for i, ev := range events {
		drop := excludedPath(ev.ManifestPath, prefixes) ||
			(len(ecosystems) > 0 && !slices.Contains(ecosystems, ev.Package.Ecosystem))
		switch {
		case drop && dropped == 0:
			out = append(make([]model.DepEvent, 0, len(events)-1), events[:i]...)
			dropped++
		case drop:
			dropped++
		case dropped > 0:
			out = append(out, ev)
		}
	}
	if dropped == 0 {
		return events, 0
	}
	return out, dropped
}

// dropExcluded returns commits with every excluded path removed from their
// Paths, and the number of path occurrences removed. Commits left with no
// paths are kept. The input commits are not modified.
func dropExcluded(commits []model.Commit, prefixes []string) ([]model.Commit, int64) {
	out := make([]model.Commit, len(commits))
	var dropped int64
	for i, c := range commits {
		kept := slices.DeleteFunc(slices.Clone(c.Paths), func(p string) bool {
			return excludedPath(p, prefixes)
		})
		if n := len(c.Paths) - len(kept); n > 0 {
			c.Paths = kept
			dropped += int64(n)
		}
		out[i] = c
	}
	return out, dropped
}
