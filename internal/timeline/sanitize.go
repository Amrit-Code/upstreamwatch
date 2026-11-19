package timeline

import (
	"strings"
	"unicode"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// sanitizeDeps drops every dependency whose package name or package URL is
// unsafe (see unsafeIdentifier) and passes the version and requirement of the
// rest through model.Redact. It returns the kept dependencies, in order, and
// the number dropped. deps is not modified.
func sanitizeDeps(deps []model.Dependency) (kept []model.Dependency, dropped int) {
	kept = make([]model.Dependency, 0, len(deps))
	for _, d := range deps {
		if unsafeIdentifier(d.Package.Name) || unsafeIdentifier(d.Package.PURL) {
			dropped++
			continue
		}
		d.Version = model.Redact(d.Version)
		d.Requirement = model.Redact(d.Requirement)
		kept = append(kept, d)
	}
	return kept, dropped
}

// unsafeIdentifier reports whether s cannot be a package name or a
// version-less package URL: it holds a control character (below 0x20 or
// 0x7f), any whitespace, "://" or "@". Such a value is a URL, possibly with
// credentials, or an attempt to inject text into a terminal or a request,
// never a package.
func unsafeIdentifier(s string) bool {
	if strings.Contains(s, "://") || strings.Contains(s, "@") {
		return true
	}
	return strings.ContainsFunc(s, func(r rune) bool {
		return r < 0x20 || r == 0x7f || unicode.IsSpace(r)
	})
}
