package gitsrc

import (
	"errors"
	"strings"
	"testing"
)

func TestChainScanner(t *testing.T) {
	t.Parallel()
	a := strings.Repeat("a", 40)
	b := strings.Repeat("b", 40)
	c := strings.Repeat("c", 40)
	out := a + "\n" + b + "\n" + c + "\n"

	tests := []struct {
		name         string
		want         string
		limit        int64
		chunk        int // bytes per Write; the want line is split across writes when small
		wantFound    bool
		wantExceeded bool
	}{
		{"match in one write", b, 1 << 20, len(out), true, false},
		{"match split across writes", c, 1 << 20, 7, true, false},
		{"no match", strings.Repeat("d", 40), 1 << 20, 5, false, false},
		{"prefix of a line is no match", b[:20], 1 << 20, len(out), false, false},
		{"match within the limit", b, 82, len(out), true, false},
		{"match past the limit", c, 82, len(out), false, true},
		{"no match past the limit", strings.Repeat("d", 40), 100, 3, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stops := 0
			s := &chainScanner{want: tt.want, limit: tt.limit, stop: func() { stops++ }}
			var stopErr error
			for rest := out; len(rest) > 0 && stopErr == nil; {
				n := min(tt.chunk, len(rest))
				written, err := s.Write([]byte(rest[:n]))
				if err != nil {
					stopErr = err
					break
				}
				if written != n {
					t.Fatalf("Write() = %d, nil, want %d", written, n)
				}
				rest = rest[n:]
			}
			if s.found != tt.wantFound || s.exceeded != tt.wantExceeded {
				t.Errorf("found, exceeded = %v, %v, want %v, %v", s.found, s.exceeded, tt.wantFound, tt.wantExceeded)
			}
			stopped := tt.wantFound || tt.wantExceeded
			if stopped && (!errors.Is(stopErr, errChainStop) || stops != 1) {
				t.Errorf("Write error = %v, stop calls = %d, want errChainStop and 1", stopErr, stops)
			}
			if !stopped && (stopErr != nil || stops != 0) {
				t.Errorf("Write error = %v, stop calls = %d, want nil and 0", stopErr, stops)
			}
			if stopped {
				if _, err := s.Write([]byte("more\n")); !errors.Is(err, errChainStop) {
					t.Errorf("Write after stop = %v, want errChainStop", err)
				}
			}
		})
	}
}
