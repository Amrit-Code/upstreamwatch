package model

import "regexp"

// redacted replaces each secret that Redact finds.
const redacted = "[REDACTED]"

// redaction is one pattern Redact applies and its replacement template.
type redaction struct {
	re   *regexp.Regexp
	repl string
}

// redactions are compiled once and only read afterwards, so Redact is safe for
// concurrent use. Each replacement leaves text that no pattern matches again
// except as itself, so the order only decides which pattern names a secret.
var redactions = []redaction{
	// URL userinfo, scheme required. The userinfo run is greedy up to the last
	// "@" before the host, so an unencoded "@" in a password is covered too.
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/?#\s]+@`), "${1}" + redacted + "@"},
	// GitHub tokens: fine-grained and classic.
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}|gh[pousr]_[A-Za-z0-9]{20,}`), redacted},
	// Anthropic and OpenAI style keys.
	{regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{10,}|sk-[A-Za-z0-9_-]{20,}`), redacted},
	// "Authorization: <scheme> <value>", header name in any case.
	{regexp.MustCompile(`((?i:authorization):[ \t]*\S+[ \t]+)\S+`), "${1}" + redacted},
	// "Bearer <value>" and "Basic <value>", capitalised as shown.
	{regexp.MustCompile(`((?:Bearer|Basic)[ \t]+)\S+`), "${1}" + redacted},
	// "x-access-token:<value>" up to the next "@", space or end.
	{regexp.MustCompile(`x-access-token:[^@\s]+`), "x-access-token:" + redacted},
}

// Redact returns s with credentials replaced by "[REDACTED]": URL userinfo,
// GitHub tokens, API keys, authorization header values and x-access-token
// values. Error text from outside tools passes through it before it is
// stored, logged or shown.
func Redact(s string) string {
	for _, r := range redactions {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
