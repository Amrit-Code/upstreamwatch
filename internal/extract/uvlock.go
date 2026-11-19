package extract

import (
	"strconv"
	"strings"
)

// uvLocalSourceKinds are the uv.lock source kinds of a package that lives in
// the project itself: its own root, a workspace member or a path dependency.
var uvLocalSourceKinds = map[string]bool{
	"editable":  true,
	"virtual":   true,
	"directory": true,
	"path":      true,
}

// uvLocalPackages returns the names of the uv.lock packages whose source is
// local. The manifests library does not report a uv package's source kind,
// and no TOML library is approved, so this relies on the layout uv writes:
// each package is a [[package]] table whose name = "..." and
// source = { kind = ... } keys sit unindented on lines of their own. A
// package whose name or source line cannot be read is not reported.
func uvLocalPackages(content []byte) map[string]bool {
	local := make(map[string]bool)
	var (
		inPackage  bool
		name, kind string
	)
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "[[package]]" {
			inPackage, name, kind = true, "", ""
			continue
		}
		// Any other table header, such as [package.metadata], ends the
		// package's own keys.
		if strings.HasPrefix(line, "[") {
			inPackage = false
			continue
		}
		if !inPackage {
			continue
		}
		if v, ok := strings.CutPrefix(line, "name = "); ok {
			if s, err := strconv.Unquote(v); err == nil {
				name = s
			}
		} else if v, ok := strings.CutPrefix(line, "source = "); ok {
			kind = inlineTableFirstKey(v)
		}
		if name != "" && uvLocalSourceKinds[kind] {
			local[name] = true
		}
	}
	return local
}

// inlineTableFirstKey returns the first key of a one-line TOML inline table
// such as { editable = "." }, or "" when the value is not one.
func inlineTableFirstKey(v string) string {
	rest, ok := strings.CutPrefix(v, "{")
	if !ok {
		return ""
	}
	key, _, ok := strings.Cut(rest, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(key)
}
