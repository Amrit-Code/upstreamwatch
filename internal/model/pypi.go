package model

import "strings"

// PyPIName returns the PEP 503 normalised form of a PyPI project name: lower
// case, with every run of "-", "_" and "." replaced by one "-". Two spellings
// of the same project ("ruamel.yaml", "Ruamel_YAML") give the same result, so
// it is the name every PyPI package URL carries.
func PyPIName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	sep := false
	for _, r := range strings.ToLower(name) {
		if r == '-' || r == '_' || r == '.' {
			sep = true
			continue
		}
		if sep {
			b.WriteByte('-')
			sep = false
		}
		b.WriteRune(r)
	}
	if sep {
		b.WriteByte('-')
	}
	return b.String()
}
