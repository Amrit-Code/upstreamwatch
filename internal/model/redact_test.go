package model

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	t.Parallel()
	// Token bodies are built from repeated characters so no committed string
	// looks like a real credential.
	pat := "github_pat_" + strings.Repeat("A1_", 10)
	ghp := "ghp_" + strings.Repeat("a1", 15)
	gho := "gho_" + strings.Repeat("B", 20)
	ant := "sk-ant-" + strings.Repeat("x-_9", 5)
	oai := "sk-" + strings.Repeat("Z9", 12)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"url user and password", "clone https://u:hunter2@git.example/x: exit 128", "clone https://[REDACTED]@git.example/x: exit 128"},
		{"url token only", "fetch https://" + ghp + "@github.com/example-org/example-server.git", "fetch https://[REDACTED]@github.com/example-org/example-server.git"},
		{"url unencoded at in password", "https://u:p@ss@host.example/x", "https://[REDACTED]@host.example/x"},
		{"url other scheme", "git+ssh://deploy:pw@git.example:22/r", "git+ssh://[REDACTED]@git.example:22/r"},
		{"github fine-grained pat", "token " + pat + " rejected", "token [REDACTED] rejected"},
		{"github classic pat", "bad credentials for " + ghp, "bad credentials for [REDACTED]"},
		{"github oauth token", gho + ".", "[REDACTED]."},
		{"anthropic key", "key=" + ant, "key=[REDACTED]"},
		{"anthropic key short tail", "key=sk-ant-abcdefghij", "key=[REDACTED]"},
		{"openai key", "using " + oai + " now", "using [REDACTED] now"},
		{"authorization header", "Authorization: token abc123 sent", "Authorization: token [REDACTED] sent"},
		{"authorization header lower case", "authorization: Custom s3cr3t", "authorization: Custom [REDACTED]"},
		{"bearer", "header Bearer abc.def-ghi rejected", "header Bearer [REDACTED] rejected"},
		{"basic", "Basic dXNlcjpwYXNz", "Basic [REDACTED]"},
		{"x-access-token before at", "x-access-token:s3cr3t@github.com", "x-access-token:[REDACTED]@github.com"},
		{"x-access-token before space", "pass x-access-token:s3cr3t to git", "pass x-access-token:[REDACTED] to git"},
		{"x-access-token at end", "x-access-token:s3cr3t", "x-access-token:[REDACTED]"},
		{
			"several secrets",
			"fetch https://u:hunter2@git.example/x: Authorization: Bearer " + ghp + " and " + ant,
			"fetch https://[REDACTED]@git.example/x: Authorization: Bearer [REDACTED] and [REDACTED]",
		},
		{"plain url unchanged", "https://github.com/org/repo", "https://github.com/org/repo"},
		{"email unchanged", "mail user@example.com", "mail user@example.com"},
		{"scheme-less userinfo unchanged", "user:pass@host", "user:pass@host"},
		{"short sk unchanged", "sk-1", "sk-1"},
		{"short github prefix unchanged", "ghp_short", "ghp_short"},
		{"lower-case basic unchanged", "basic idea", "basic idea"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Redact(tt.in); got != tt.want {
				t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
