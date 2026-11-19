package model

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
	// Embedded zone data, so Europe/London loads on machines without it.
	_ "time/tzdata"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// assertGolden compares got with testdata/<name>.golden, rewriting the file
// first when -update is set.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create it): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from %s; rerun with -update and review the diff\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func london(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load Europe/London: %v", err)
	}
	return loc
}

// sampleRuns are stored in UTC; in July London is one hour ahead, so the
// rendered times show whether the digest converts them.
func sampleRuns() []Run {
	return []Run{
		{Job: "digest", StartedAt: time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC), Status: RunRunning},
		{Job: "ingest", StartedAt: time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 7, 14, 6, 5, 0, 0, time.UTC), Status: RunSucceeded},
		{Job: "vulns", StartedAt: time.Date(2026, 7, 14, 6, 8, 0, 0, time.UTC), FinishedAt: time.Date(2026, 7, 14, 6, 10, 30, 0, time.UTC), Status: RunFailed},
	}
}

func sampleDigest(t *testing.T) Digest {
	return Digest{
		GeneratedAt: time.Date(2026, 7, 14, 8, 0, 0, 0, london(t)),
		New: []DigestItem{
			{
				FindingID: 1842, Kind: KindAdvisory, Score: 114, Tag: "[A]",
				Headline: "high  GO-2026-4866  pkg:golang/toolchain 1.26.0  (repo: server)",
				Lines: []string{
					"net/http: request smuggling via chunked encoding. Fixed in 1.26.4. KEV: no. EPSS p71.",
					"Exposed since 2026-05-14 (go.mod) — confidence high",
				},
				URL: "https://upstreamwatch.example.ts.net/f/1842",
			},
			{
				FindingID: 1850, Kind: KindJobFailure, Score: JobFailureScore, Tag: "[J]",
				Headline: "vulns failed",
				Lines:    []string{"query advisories: status 503"},
				URL:      "https://upstreamwatch.example.ts.net/f/1850",
			},
			{
				FindingID: 1851, Kind: KindAdvisory, Score: 40, Tag: "[A]",
				Headline: "low  GHSA-xxxx-yyyy-zzzz  pkg:npm/left-pad 1.3.0  (repo: web)",
				URL:      "https://upstreamwatch.example.ts.net/f/1851",
			},
		},
		Changed: []DigestItem{
			{
				FindingID: 1700, Kind: KindAdvisory, Score: 60, Tag: "[A]",
				Headline: "medium  PYSEC-2026-2275  pkg:pypi/requests 2.31.0  (repo: tools)",
				Lines:    []string{"now has a fixed version (2.32.5)"},
				URL:      "https://upstreamwatch.example.ts.net/f/1700",
			},
		},
		Waiting:     2,
		LastRuns:    sampleRuns(),
		Open:        11,
		Acked:       4,
		Snoozed:     2,
		FindingsURL: "https://upstreamwatch.example.ts.net/",
	}
}

func TestDigestPlainText_Golden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		digest func(t *testing.T) Digest
	}{
		{"full", sampleDigest},
		{"no changed no waiting", func(t *testing.T) Digest {
			d := sampleDigest(t)
			d.Changed = nil
			d.Waiting = 0
			return d
		}},
		{"without base url", func(t *testing.T) Digest {
			d := sampleDigest(t)
			d.FindingsURL = ""
			for _, items := range [][]DigestItem{d.New, d.Changed} {
				for i := range items {
					items[i].URL = ""
				}
			}
			return d
		}},
		{"no runs", func(t *testing.T) Digest {
			d := sampleDigest(t)
			d.New = d.New[:1]
			d.Changed = nil
			d.LastRuns = nil
			return d
		}},
		{"empty fields", func(t *testing.T) Digest {
			return Digest{
				GeneratedAt: time.Date(2026, 7, 14, 8, 0, 0, 0, london(t)),
				New:         []DigestItem{{}},
				LastRuns:    []Run{{Job: "issues"}},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertGolden(t, t.Name(), []byte(tt.digest(t).PlainText()))
		})
	}
}

func TestDigestPlainText_Heartbeat_Golden(t *testing.T) {
	t.Parallel()
	d := Digest{
		GeneratedAt: time.Date(2026, 11, 16, 8, 0, 0, 0, london(t)),
		Heartbeat:   true,
		LastRuns: []Run{
			{Job: "ingest", FinishedAt: time.Date(2026, 11, 16, 7, 5, 0, 0, time.UTC), Status: RunSucceeded},
			{Job: "vulns", FinishedAt: time.Date(2026, 11, 16, 7, 10, 0, 0, time.UTC), Status: RunSucceeded},
		},
		Open:        3,
		Acked:       1,
		FindingsURL: "https://upstreamwatch.example.ts.net/",
	}
	assertGolden(t, t.Name(), []byte(d.PlainText()))
}

func TestDigestPlainText_More(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		digest func(t *testing.T) Digest
	}{
		{"with url", func(t *testing.T) Digest {
			d := sampleDigest(t)
			d.MoreNew = 7
			d.MoreChanged = 1
			return d
		}},
		{"without url", func(t *testing.T) Digest {
			d := sampleDigest(t)
			d.FindingsURL = ""
			d.MoreNew = 7
			return d
		}},
		{"only more in a section", func(t *testing.T) Digest {
			d := sampleDigest(t)
			d.New = nil
			d.MoreNew = 4
			d.Changed = nil
			return d
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertGolden(t, t.Name(), []byte(tt.digest(t).PlainText()))
		})
	}
}

func TestDigestSubject(t *testing.T) {
	t.Parallel()
	loc := london(t)
	// 23:30 UTC on 13 July is 00:30 on 14 July in London: the date follows
	// GeneratedAt's location.
	late := time.Date(2026, 7, 13, 23, 30, 0, 0, time.UTC).In(loc)
	tests := []struct {
		name string
		d    Digest
		want string
	}{
		{"counts include more", Digest{GeneratedAt: late, New: make([]DigestItem, 3), MoreNew: 2, Changed: make([]DigestItem, 1), MoreChanged: 1}, "upstreamwatch — 2026-07-14 — 5 new, 2 changed"},
		{"nothing", Digest{GeneratedAt: late}, "upstreamwatch — 2026-07-14 — 0 new, 0 changed"},
		{"heartbeat", Digest{GeneratedAt: late, Heartbeat: true}, "upstreamwatch — 2026-07-14 — all quiet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.d.Subject(); got != tt.want {
				t.Errorf("Subject() = %q, want %q", got, tt.want)
			}
		})
	}
}
