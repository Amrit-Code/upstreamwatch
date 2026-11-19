package httpx

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestRequestedWait(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		status   int
		header   map[string]string
		wantWait time.Duration
		wantOK   bool
	}{
		{"delta seconds", 503, map[string]string{"Retry-After": "7"}, 7 * time.Second, true},
		{"http date", 429, map[string]string{"Retry-After": t0.Add(90 * time.Second).Format(http.TimeFormat)}, 90 * time.Second, true},
		{"date in the past", 429, map[string]string{"Retry-After": t0.Add(-time.Hour).Format(http.TimeFormat)}, 0, true},
		{"negative delta ignored", 503, map[string]string{"Retry-After": "-5"}, 0, false},
		{"reset on rate-limited 403", 403, map[string]string{"X-Ratelimit-Remaining": "0", "X-Ratelimit-Reset": unixAt(t0.Add(42 * time.Second))}, 42 * time.Second, true},
		{"reset ignored on 503", 503, map[string]string{"X-Ratelimit-Reset": unixAt(t0.Add(42 * time.Second))}, 0, false},
		{"retry-after wins over reset", 429, map[string]string{"Retry-After": "3", "X-Ratelimit-Reset": unixAt(t0.Add(42 * time.Second))}, 3 * time.Second, true},
		{"nothing asked", 503, nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := http.Header{}
			for k, v := range tt.header {
				h.Set(k, v)
			}
			gotWait, gotOK := requestedWait(tt.status, h, t0)
			if gotWait != tt.wantWait || gotOK != tt.wantOK {
				t.Errorf("requestedWait(%d, %v) = %v, %v; want %v, %v", tt.status, tt.header, gotWait, gotOK, tt.wantWait, tt.wantOK)
			}
		})
	}
}

func TestBackoffCeiling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		attempt int
		maxWait time.Duration
		want    time.Duration
	}{
		{1, 30 * time.Second, 500 * time.Millisecond},
		{2, 30 * time.Second, time.Second},
		{3, 30 * time.Second, 2 * time.Second},
		{10, 30 * time.Second, 30 * time.Second},
		{1000, 30 * time.Second, 30 * time.Second},
		{1, 100 * time.Millisecond, 100 * time.Millisecond},
	}
	for _, tt := range tests {
		got := backoffCeiling(tt.attempt, tt.maxWait)
		if got != tt.want {
			t.Errorf("backoffCeiling(%d, %v) = %v, want %v", tt.attempt, tt.maxWait, got, tt.want)
		}
	}
}

func TestFullJitter(t *testing.T) {
	t.Parallel()
	if got := fullJitter(0); got != 0 {
		t.Errorf("fullJitter(0) = %v, want 0", got)
	}
	for range 100 {
		if got := fullJitter(time.Second); got < 0 || got >= time.Second {
			t.Fatalf("fullJitter(1s) = %v, want in [0, 1s)", got)
		}
	}
}

// unixAt formats t as x-ratelimit-reset does: Unix seconds.
func unixAt(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}
