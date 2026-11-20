package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
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

// cet is one hour ahead of UTC, so a date rendered near midnight shows
// whether the digest's location is used.
var cet = time.FixedZone("CET", 3600)

func utc(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

func sampleAdvisoryDetails() []struct {
	detail  model.FindingDetail
	changed bool
} {
	open := model.FindingDetail{
		Finding: model.Finding{
			ID: 1842, Kind: model.KindAdvisory, DepVersionID: 7, AdvisoryID: "GO-2026-4866", Score: 114,
			RankReason: "high 70; EPSS p71 +14; direct +10; exposed now +20",
		},
		RepoSlug: "example-server",
		Package:  model.Package{PURL: "pkg:golang/toolchain"},
		Interval: model.DepInterval{ID: 7, ManifestPath: "go.mod", Version: "1.26.0", From: utc(2026, 2, 1, 9)},
		Advisory: model.Advisory{
			ID: "GO-2026-4866", Severity: model.SeverityHigh,
			Summary: "net/http: request\x1b[31m smuggling\n\nvia  chunked\r\n\tencoding\x1b[0m",
			HasEPSS: true, EPSS: 0.1234, EPSSPercentile: 0.712,
		},
		Exposures: []model.Exposure{
			{
				Match:    model.AdvisoryMatch{ExposedFrom: utc(2025, 11, 1, 0), ExposedTo: utc(2026, 1, 15, 0), Confidence: model.ConfidenceHigh, FixedVersion: "1.25.9"},
				Interval: model.DepInterval{ID: 5, ManifestPath: "go.mod", Version: "1.25.1", From: utc(2025, 11, 1, 0)},
			},
			{
				Match:    model.AdvisoryMatch{ExposedFrom: utc(2026, 5, 14, 23), Confidence: model.ConfidenceHigh, FixedVersion: "1.26.4"},
				Interval: model.DepInterval{ID: 7, ManifestPath: "go.mod", Version: "1.26.0", From: utc(2026, 2, 1, 9)},
			},
		},
	}
	historical := model.FindingDetail{
		Finding: model.Finding{
			ID: 1900, Kind: model.KindAdvisory, DepVersionID: 11, AdvisoryID: "GHSA-aaaa-bbbb-cccc", Score: 75,
			RankReason: "unknown 30; KEV +30; direct +10; confidence low -25",
		},
		RepoSlug: "example-web",
		Package:  model.Package{PURL: "pkg:npm/example-parser"},
		Interval: model.DepInterval{ID: 11, ManifestPath: "web/package-lock.json", Version: "2.0.1"},
		Advisory: model.Advisory{
			ID:      "GHSA-aaaa-bbbb-cccc",
			Details: "Parsing a crafted\ndocument panics. A second sentence follows.",
			KEV:     true,
		},
		Exposures: []model.Exposure{{
			Match:    model.AdvisoryMatch{ExposedFrom: utc(2025, 6, 1, 12), ExposedTo: utc(2025, 9, 30, 23), Confidence: model.ConfidenceLow},
			Interval: model.DepInterval{ID: 11, ManifestPath: "web/package-lock.json", Version: "2.0.1", From: utc(2025, 6, 1, 12)},
		}},
	}
	long := model.FindingDetail{
		Finding:  model.Finding{ID: 1950, Kind: model.KindAdvisory, AdvisoryID: "PYSEC-2026-1", Score: 40},
		RepoSlug: "example-tools",
		Package:  model.Package{PURL: "pkg:pypi/example-lib"},
		Advisory: model.Advisory{
			ID: "PYSEC-2026-1", Severity: model.SeverityMedium,
			Summary: strings.Repeat("ab ", 100) + "\xff tail",
		},
	}
	return []struct {
		detail  model.FindingDetail
		changed bool
	}{{open, false}, {historical, true}, {long, false}}
}

func TestRender_Advisory_Golden(t *testing.T) {
	t.Parallel()
	var items []model.DigestItem
	for _, s := range sampleAdvisoryDetails() {
		item, err := renderItem(s.detail, s.changed, "https://upw.example.test", cet)
		if err != nil {
			t.Fatalf("renderItem(%d) error = %v", s.detail.Finding.ID, err)
		}
		items = append(items, item)
	}
	for _, it := range items {
		for _, l := range append([]string{it.Headline}, it.Lines...) {
			if strings.ContainsAny(l, "\x1b\r\n\t") || !utf8.ValidString(l) {
				t.Errorf("item %d line %q is not sanitised", it.FindingID, l)
			}
		}
	}
	if want := "net/http: request [31m smuggling via chunked encoding [0m Fixed in 1.26.4. KEV: no. EPSS 0.12 (p71)."; items[0].Lines[0] != want {
		t.Errorf("summary line = %q, want %q", items[0].Lines[0], want)
	}
	got, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		t.Fatalf("marshal items: %v", err)
	}
	assertGolden(t, t.Name(), append(got, '\n'))
}

// TestRender_DefangsUpstreamURLs_Golden checks that URLs in advisory text
// reach the digest broken, while the package URL, repo slug, advisory id and
// finding URL stay as they are.
func TestRender_DefangsUpstreamURLs_Golden(t *testing.T) {
	t.Parallel()
	summary := model.FindingDetail{
		Finding: model.Finding{
			ID: 2001, Kind: model.KindAdvisory, DepVersionID: 9, AdvisoryID: "GHSA-pppp-qqqq-rrrr", Score: 80,
			RankReason: "high 70; direct +10",
		},
		RepoSlug: "example-server",
		Package:  model.Package{PURL: "pkg:golang/example.com/lib"},
		Interval: model.DepInterval{ID: 9, ManifestPath: "go.mod", Version: "1.0.0", From: utc(2026, 3, 1, 9)},
		Advisory: model.Advisory{
			ID: "GHSA-pppp-qqqq-rrrr", Severity: model.SeverityHigh,
			Summary: "Sign in at https://phish.example/login or HTTP://x.example to fix",
		},
		Exposures: []model.Exposure{{
			Match:    model.AdvisoryMatch{ExposedFrom: utc(2026, 3, 1, 9), Confidence: model.ConfidenceHigh, FixedVersion: "ftp://fix.example/1.0.1"},
			Interval: model.DepInterval{ID: 9, ManifestPath: "go.mod", Version: "1.0.0", From: utc(2026, 3, 1, 9)},
		}},
	}
	details := model.FindingDetail{
		Finding:  model.Finding{ID: 2002, Kind: model.KindAdvisory, AdvisoryID: "GO-2026-0002", Score: 40},
		RepoSlug: "example-tools",
		Package:  model.Package{PURL: "pkg:golang/example.com/other"},
		Advisory: model.Advisory{
			ID:      "GO-2026-0002",
			Details: "See hTtPs://evil.example/x and git+ssh://host.example/r. More at http://later.example.",
		},
	}
	var items []model.DigestItem
	for _, d := range []model.FindingDetail{summary, details} {
		item, err := renderItem(d, false, "https://upw.example.test", time.UTC)
		if err != nil {
			t.Fatalf("renderItem(%d) error = %v", d.Finding.ID, err)
		}
		items = append(items, item)
	}
	for _, it := range items {
		for _, l := range append([]string{it.Headline}, it.Lines...) {
			if strings.Contains(strings.ToLower(l), "http://") || strings.Contains(strings.ToLower(l), "https://") {
				t.Errorf("item %d line %q holds a live URL", it.FindingID, l)
			}
		}
	}
	if want := "Sign in at hxxps[://]phish.example/login or hxxp[://]x.example to fix Fixed in ftp[://]fix.example/1.0.1. KEV: no."; items[0].Lines[0] != want {
		t.Errorf("summary line = %q, want %q", items[0].Lines[0], want)
	}
	if want := "See hxxps[://]evil.example/x and git+ssh[://]host.example/r. KEV: no."; items[1].Lines[0] != want {
		t.Errorf("details line = %q, want %q", items[1].Lines[0], want)
	}
	if want := "high  GHSA-pppp-qqqq-rrrr  pkg:golang/example.com/lib 1.0.0  (repo: example-server)"; items[0].Headline != want {
		t.Errorf("headline = %q, want %q", items[0].Headline, want)
	}
	if want := "https://upw.example.test/f/2001"; items[0].URL != want {
		t.Errorf("URL = %q, want %q", items[0].URL, want)
	}
	got, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		t.Fatalf("marshal items: %v", err)
	}
	assertGolden(t, t.Name(), append(got, '\n'))
}

// TestRender_ChangedAdvisoryDefangsEveryLine checks a CHANGED item whose
// rank reason quotes a URL-bearing fixed version: no emitted line holds a
// live URL, the "Now:" line included.
func TestRender_ChangedAdvisoryDefangsEveryLine(t *testing.T) {
	t.Parallel()
	const fixed = "2.0-https://phish.example/login"
	d := model.FindingDetail{
		Finding: model.Finding{
			ID: 2003, Kind: model.KindAdvisory, DepVersionID: 12, AdvisoryID: "GHSA-ssss-tttt-uuuu", Score: 70,
			RankReason: "high 70; now has a fixed version (" + fixed + ")",
		},
		RepoSlug: "example-server",
		Package:  model.Package{PURL: "pkg:maven/org.example/lib"},
		Interval: model.DepInterval{ID: 12, ManifestPath: "pom.xml", Version: "1.0", From: utc(2026, 4, 1, 9)},
		Advisory: model.Advisory{ID: "GHSA-ssss-tttt-uuuu", Severity: model.SeverityHigh, Summary: "Lib mishandles input"},
		Exposures: []model.Exposure{{
			Match:    model.AdvisoryMatch{ExposedFrom: utc(2026, 4, 1, 9), Confidence: model.ConfidenceHigh, FixedVersion: fixed},
			Interval: model.DepInterval{ID: 12, ManifestPath: "pom.xml", Version: "1.0", From: utc(2026, 4, 1, 9)},
		}},
	}
	item, err := renderItem(d, true, "https://upw.example.test", time.UTC)
	if err != nil {
		t.Fatalf("renderItem() error = %v", err)
	}
	for _, l := range append([]string{item.Headline}, item.Lines...) {
		if strings.Contains(strings.ReplaceAll(l, "[://]", ""), "://") {
			t.Errorf("line %q holds a live URL", l)
		}
	}
	want := []string{
		"Lib mishandles input Fixed in 2.0-hxxps[://]phish.example/login. KEV: no.",
		"Exposed since 2026-04-01 (pom.xml) — confidence high",
		"Now: high 70; now has a fixed version (2.0-hxxps[://]phish.example/login)",
	}
	if !reflect.DeepEqual(item.Lines, want) {
		t.Errorf("lines = %q, want %q", item.Lines, want)
	}
}

func TestDefangURLs(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{name: "https", in: "go to https://phish.example/login now", want: "go to hxxps[://]phish.example/login now"},
		{name: "http upper case", in: "HTTP://x.example", want: "hxxp[://]x.example"},
		{name: "mixed case https", in: "hTtPs://x.example", want: "hxxps[://]x.example"},
		{name: "other scheme", in: "ftp://x.example and git+ssh://y.example", want: "ftp[://]x.example and git+ssh[://]y.example"},
		{name: "scheme glued to a word", in: "xhttps://x.example", want: "xhxxps[://]x.example"},
		{name: "separator alone", in: "a :// b", want: "a [://] b"},
		{name: "two in a row", in: "https://a.example/https://b.example", want: "hxxps[://]a.example/hxxps[://]b.example"},
		{name: "no URL", in: "pkg:golang/example.com/lib GO-2026-1", want: "pkg:golang/example.com/lib GO-2026-1"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := defangURLs(tt.in); got != tt.want {
				t.Errorf("defangURLs(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRenderItem_OtherKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		f    model.Finding
		want model.DigestItem
	}{
		{
			name: "job failure",
			f: model.Finding{ID: 3, Kind: model.KindJobFailure, Fingerprint: "job_failure|vulns", Score: 50,
				RankReason: "job vulns failed: query osv:\nstatus 503"},
			want: model.DigestItem{FindingID: 3, Kind: model.KindJobFailure, Score: 50, Tag: "[J]",
				Headline: "job vulns failed", Lines: []string{"job vulns failed: query osv: status 503"},
				URL: "https://upw.example.test/f/3"},
		},
		{
			name: "issue",
			f:    model.Finding{ID: 4, Kind: model.KindIssue, Score: 60, RankReason: "affects (0.82)"},
			want: model.DigestItem{FindingID: 4, Kind: model.KindIssue, Score: 60, Tag: "[I]",
				Headline: "affects (0.82)", URL: "https://upw.example.test/f/4"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := renderItem(model.FindingDetail{Finding: tt.f}, false, "https://upw.example.test", time.UTC)
			if err != nil {
				t.Fatalf("renderItem() error = %v", err)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tt.want)
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("renderItem() = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestRenderItem_NoBaseURLAndUnknownKind(t *testing.T) {
	t.Parallel()
	item, err := renderItem(model.FindingDetail{Finding: model.Finding{ID: 1, Kind: model.KindIssue, RankReason: "x"}}, false, "", time.UTC)
	if err != nil {
		t.Fatalf("renderItem() error = %v", err)
	}
	if item.URL != "" {
		t.Errorf("URL = %q, want empty", item.URL)
	}
	if _, err := renderItem(model.FindingDetail{Finding: model.Finding{ID: 2, Kind: "other"}}, false, "", time.UTC); err == nil {
		t.Error("renderItem(kind other) error = nil, want an error")
	}
}

func TestSanitizeLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{name: "plain", in: "high  GO-1  pkg:x 1.0", want: "high  GO-1  pkg:x 1.0"},
		{name: "controls become spaces", in: "a\r\nb\tc\x1bd\x7fe\u0085f", want: "a  b c d e f"},
		{name: "invalid utf-8", in: "a\xffb", want: "a�b"},
		{name: "exactly 300 runes", in: strings.Repeat("é", 300), want: strings.Repeat("é", 300)},
		{name: "301 runes", in: strings.Repeat("é", 301), want: strings.Repeat("é", 299) + "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeLine(tt.in); got != tt.want {
				t.Errorf("sanitizeLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCleanUpstream(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{in: "  a  b\n\n c\t", want: "a b c"},
		{in: "x\x1b[1my", want: "x [1my"},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		if got := cleanUpstream(tt.in); got != tt.want {
			t.Errorf("cleanUpstream(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFirstSentence(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{in: "One. Two.", want: "One."},
		{in: "Version 1.2.3 is bad! More.", want: "Version 1.2.3 is bad!"},
		{in: "No full stop", want: "No full stop"},
		{in: "Ends here.", want: "Ends here."},
	}
	for _, tt := range tests {
		if got := firstSentence(tt.in); got != tt.want {
			t.Errorf("firstSentence(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{in: "abc", n: 3, want: "abc"},
		{in: "abcd", n: 3, want: "ab…"},
		{in: "日本語テキスト", n: 4, want: "日本語…"},
		{in: "", n: 3, want: ""},
	}
	for _, tt := range tests {
		if got := truncateRunes(tt.in, tt.n); got != tt.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}
