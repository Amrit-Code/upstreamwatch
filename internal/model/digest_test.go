package model

import (
	"testing"
	"time"
)

func TestDigest_IsEmpty(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	item := DigestItem{FindingID: 1, Kind: KindAdvisory, Tag: "[A]", Headline: "high  GO-2026-0001"}
	tests := []struct {
		name string
		d    Digest
		want bool
	}{
		{"zero digest", Digest{}, true},
		{"counts and runs only", Digest{GeneratedAt: at, Open: 3, Acked: 2, Snoozed: 1,
			LastRuns: []Run{{ID: 1, Job: "vulns", Status: RunSucceeded}}, FindingsURL: "https://upw.example.test/"}, true},
		{"heartbeat without items", Digest{GeneratedAt: at, Heartbeat: true}, true},
		{"waiting candidates are not items", Digest{GeneratedAt: at, Waiting: 4}, true},
		{"empty slices", Digest{New: []DigestItem{}, Changed: []DigestItem{}}, true},
		{"one new item", Digest{New: []DigestItem{item}}, false},
		{"one changed item", Digest{Changed: []DigestItem{item}}, false},
		{"only more new", Digest{MoreNew: 1}, false},
		{"only more changed", Digest{MoreChanged: 1}, false},
	}
	for _, tt := range tests {
		if got := tt.d.IsEmpty(); got != tt.want {
			t.Errorf("%s: IsEmpty() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
