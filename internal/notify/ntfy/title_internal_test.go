package ntfy

import "testing"

func TestASCIITitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "ascii unchanged", in: "upstreamwatch 2026-11-18", want: "upstreamwatch 2026-11-18"},
		{name: "em dash becomes hyphen", in: "a — b", want: "a - b"},
		{name: "other non-ascii dropped", in: "café ✓ naïve…日本", want: "caf  nave"},
		{name: "mixed", in: "upstreamwatch — 2026-11-18 — résumé", want: "upstreamwatch - 2026-11-18 - rsum"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := asciiTitle(tt.in); got != tt.want {
				t.Errorf("asciiTitle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
