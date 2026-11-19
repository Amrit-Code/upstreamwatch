package model

import "log/slog"

// Secret is a credential read from the environment. It prints, logs and
// marshals as its last four characters so it cannot leak through fmt, slog or JSON.
type Secret string

// String returns the redacted form.
func (s Secret) String() string { return redact(string(s)) }

// GoString keeps %#v redacted.
func (s Secret) GoString() string { return redact(string(s)) }

// LogValue keeps slog output redacted.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redact(string(s))) }

// MarshalText keeps JSON and text encodings redacted.
func (s Secret) MarshalText() ([]byte, error) { return []byte(redact(string(s))), nil }

// Reveal returns the value. Call it only where the credential is sent:
// an Authorization header, SMTP auth, the provider client.
func (s Secret) Reveal() string { return string(s) }

func redact(v string) string {
	switch {
	case v == "":
		return ""
	case len(v) <= 8:
		return "****"
	default:
		return "****" + v[len(v)-4:]
	}
}
