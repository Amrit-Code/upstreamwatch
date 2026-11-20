package osv

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestToPackage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ecosystem, name string
		want            model.Package
		wantOK          bool
	}{
		{"Go", "stdlib", model.Package{PURL: "pkg:golang/toolchain", Ecosystem: "golang", Name: "toolchain"}, true},
		{"Go", "toolchain", model.Package{PURL: "pkg:golang/toolchain", Ecosystem: "golang", Name: "toolchain"}, true},
		{"Go", "github.com/gin-gonic/gin", model.Package{PURL: "pkg:golang/github.com/gin-gonic/gin", Ecosystem: "golang", Name: "github.com/gin-gonic/gin"}, true},
		{"PyPI", "PyYAML", model.Package{PURL: "pkg:pypi/pyyaml", Ecosystem: "pypi", Name: "pyyaml"}, true},
		{"PyPI", "Zope.Interface__Extra--x", model.Package{PURL: "pkg:pypi/zope-interface-extra-x", Ecosystem: "pypi", Name: "zope-interface-extra-x"}, true},
		{"GitHub Actions", "actions/checkout", model.Package{PURL: "pkg:githubactions/actions/checkout", Ecosystem: "github-actions", Name: "actions/checkout"}, true},
		{"npm", "@scope/name", model.Package{PURL: "pkg:npm/%40scope/name", Ecosystem: "npm", Name: "@scope/name"}, true},
		{"npm", "lodash", model.Package{PURL: "pkg:npm/lodash", Ecosystem: "npm", Name: "lodash"}, true},
		{"crates.io", "serde", model.Package{PURL: "pkg:cargo/serde", Ecosystem: "cargo", Name: "serde"}, true},
		{"RubyGems", "rack", model.Package{PURL: "pkg:gem/rack", Ecosystem: "gem", Name: "rack"}, true},
		{"Maven", "org.apache.tomcat:tomcat-coyote", model.Package{PURL: "pkg:maven/org.apache.tomcat/tomcat-coyote", Ecosystem: "maven", Name: "org.apache.tomcat/tomcat-coyote"}, true},
		{"Maven", "tomcat-coyote", model.Package{}, false},
		{"Maven", ":tomcat-coyote", model.Package{}, false},
		{"Maven", "org.apache.tomcat:", model.Package{}, false},
		{"Maven", "org.apache.tomcat:tomcat-coyote:jar", model.Package{}, false},
		{"Debian:12", "openssl", model.Package{}, false},
		{"Alpine", "openssl", model.Package{}, false},
		{"", "", model.Package{}, false},
		{"Go", "", model.Package{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.ecosystem+"/"+tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := toPackage(tt.ecosystem, tt.name)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("toPackage(%q, %q) = %+v, %v, want %+v, %v", tt.ecosystem, tt.name, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestToPackage_EcosystemSuffixIgnored(t *testing.T) {
	t.Parallel()
	got, ok := toPackage("PyPI:extra", "Requests")
	if want := (model.Package{PURL: "pkg:pypi/requests", Ecosystem: "pypi", Name: "requests"}); !ok || got != want {
		t.Errorf("toPackage(PyPI:extra, Requests) = %+v, %v, want %+v, true", got, ok, want)
	}
}

func TestPurlTypes_AllKnownToModel(t *testing.T) {
	t.Parallel()
	for _, typ := range purlTypes {
		if _, ok := model.OSVEcosystem("pkg:" + typ + "/x"); !ok {
			t.Errorf("purl type %q has no OSV ecosystem in model", typ)
		}
	}
}

func TestParseRecord_Severity(t *testing.T) {
	t.Parallel()
	const head = `{"id":"GHSA-xxxx-xxxx-xxxx","modified":"2026-01-01T00:00:00Z",`
	tests := []struct {
		name       string
		body       string
		wantLabel  model.SeverityLabel
		wantScore  float64
		wantVector string
	}{
		{
			name:       "CVSS only",
			body:       head + `"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]}`,
			wantLabel:  model.SeverityCritical,
			wantScore:  9.8,
			wantVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		},
		{
			name:       "database severity wins over the score",
			body:       head + `"database_specific":{"severity":"low"},"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]}`,
			wantLabel:  model.SeverityLow,
			wantScore:  9.8,
			wantVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		},
		{
			name:      "MEDIUM",
			body:      head + `"database_specific":{"severity":"Medium"}}`,
			wantLabel: model.SeverityMedium,
		},
		{
			name:      "unrecognised database severity falls back to the score",
			body:      head + `"database_specific":{"severity":"SEVERE"},"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N"}]}`,
			wantLabel: model.SeverityMedium, wantScore: 5.3, wantVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N",
		},
		{
			name:      "non-string database severity is ignored",
			body:      head + `"database_specific":{"severity":3}}`,
			wantLabel: model.SeverityUnknown,
		},
		{
			name:      "CVSS v4 only",
			body:      head + `"severity":[{"type":"CVSS_V4","score":"CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N"}]}`,
			wantLabel: model.SeverityUnknown,
		},
		{
			name:      "malformed vector",
			body:      head + `"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L"}]}`,
			wantLabel: model.SeverityUnknown,
		},
		{
			name:      "only the first CVSS_V3 entry counts",
			body:      head + `"severity":[{"type":"CVSS_V3","score":"garbage"},{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]}`,
			wantLabel: model.SeverityUnknown,
		},
		{
			name:       "zero score has no label",
			body:       head + `"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N"}]}`,
			wantLabel:  model.SeverityUnknown,
			wantVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N",
		},
		{
			name:      "no severity",
			body:      head + `"summary":"x"}`,
			wantLabel: model.SeverityUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseRecord([]byte(tt.body))
			if err != nil {
				t.Fatalf("parseRecord() error = %v", err)
			}
			if got.Severity != tt.wantLabel || got.CVSSScore != tt.wantScore || got.CVSSVector != tt.wantVector {
				t.Errorf("parseRecord() severity = %q, %v, %q, want %q, %v, %q",
					got.Severity, got.CVSSScore, got.CVSSVector, tt.wantLabel, tt.wantScore, tt.wantVector)
			}
		})
	}
}

func TestParseRecord_AffectedAndTimes(t *testing.T) {
	t.Parallel()
	body := []byte(`{"id":"PYSEC-2026-1","modified":"2026-02-03T04:05:06+02:00","withdrawn":"2026-02-04T00:00:00.25Z",
		"affected":[
			{"package":{"ecosystem":"Debian:12","name":"python3"}},
			{"package":{"ecosystem":"PyPI","name":"Some_Pkg"},
			 "ranges":[{"type":"GIT","repo":"https://example.com/example-org/example-server","events":[{"introduced":"abc"},{"last_affected":"def"},{"limit":"fff"}]}],
			 "versions":["1.0"],"ecosystem_specific":null}
		]}`)
	got, err := parseRecord(body)
	if err != nil {
		t.Fatalf("parseRecord() error = %v", err)
	}
	if want := time.Date(2026, 2, 3, 2, 5, 6, 0, time.UTC); !got.Modified.Equal(want) || got.Modified.Location() != time.UTC {
		t.Errorf("Modified = %v, want %v in UTC", got.Modified, want)
	}
	if want := time.Date(2026, 2, 4, 0, 0, 0, 25e7, time.UTC); !got.Withdrawn.Equal(want) || !got.IsWithdrawn() {
		t.Errorf("Withdrawn = %v, want %v", got.Withdrawn, want)
	}
	if !got.Published.IsZero() {
		t.Errorf("Published = %v, want zero", got.Published)
	}
	if len(got.Affected) != 1 {
		t.Fatalf("Affected = %+v, want the Debian entry dropped", got.Affected)
	}
	a := got.Affected[0]
	if a.Package.PURL != "pkg:pypi/some-pkg" || a.SourceDB != "PYSEC" || a.EcosystemSpecific != nil || a.DatabaseSpecific != nil {
		t.Errorf("affected = %+v", a)
	}
	wantRange := model.AffectedRange{
		Type: model.RangeGit,
		Repo: "https://example.com/example-org/example-server",
		Events: []model.RangeEvent{
			{Introduced: "abc"}, {LastAffected: "def"}, {Limit: "fff"},
		},
	}
	if !reflect.DeepEqual(a.Ranges, []model.AffectedRange{wantRange}) {
		t.Errorf("Ranges = %+v, want [%+v]", a.Ranges, wantRange)
	}
}

func TestParseRecord_MissingModified(t *testing.T) {
	t.Parallel()
	if _, err := parseRecord([]byte(`{"id":"GO-1"}`)); err == nil {
		t.Error("parseRecord() error = nil, want an error for a missing modified time")
	}
}

func TestCapDetails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{name: "short", in: "a short text", want: "a short text"},
		{name: "exactly the cap", in: strings.Repeat("x", detailsMaxBytes), want: strings.Repeat("x", detailsMaxBytes)},
		{name: "one byte over", in: strings.Repeat("x", detailsMaxBytes+1), want: strings.Repeat("x", detailsMaxBytes) + "…"},
		// "é" is two bytes; with one "x" in front the cap falls inside a
		// rune, so the cut moves back to the rune's start.
		{name: "cap inside a rune", in: "x" + strings.Repeat("é", detailsMaxBytes/2), want: "x" + strings.Repeat("é", detailsMaxBytes/2-1) + "…"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := capDetails(tt.in)
			if got != tt.want {
				t.Errorf("capDetails(%d bytes) = %d bytes ending %q, want %d bytes ending %q",
					len(tt.in), len(got), got[max(0, len(got)-8):], len(tt.want), tt.want[max(0, len(tt.want)-8):])
			}
			if !utf8.ValidString(got) {
				t.Errorf("capDetails(%d bytes) is not valid UTF-8", len(tt.in))
			}
		})
	}
}

func TestParseRecord_LongDetailsCutRawKept(t *testing.T) {
	t.Parallel()
	details := strings.Repeat("d", detailsMaxBytes+100)
	body := []byte(`{"id":"GO-1","modified":"2026-01-01T00:00:00Z","details":"` + details + `"}`)
	got, err := parseRecord(body)
	if err != nil {
		t.Fatalf("parseRecord() error = %v", err)
	}
	if want := details[:detailsMaxBytes] + "…"; got.Details != want {
		t.Errorf("Details is %d bytes, want %d bytes ending in …", len(got.Details), len(want))
	}
	if !bytes.Equal(got.Raw, body) {
		t.Error("Raw differs from the body; it must keep the exact record")
	}
}

func TestNormalizePyPI(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"PyYAML", "pyyaml"},
		{"zope.interface", "zope-interface"},
		{"a__b--c..d", "a-b-c-d"},
		{"a-_.b", "a-b"},
		{"_lead", "-lead"},
		{"trail.", "trail-"},
		{"requests", "requests"},
	}
	for _, tt := range tests {
		if got := normalizePyPI(tt.in); got != tt.want {
			t.Errorf("normalizePyPI(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestScoreLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		score float64
		want  model.SeverityLabel
	}{
		{10, model.SeverityCritical},
		{9.0, model.SeverityCritical},
		{8.9, model.SeverityHigh},
		{7.0, model.SeverityHigh},
		{6.9, model.SeverityMedium},
		{4.0, model.SeverityMedium},
		{3.9, model.SeverityLow},
		{0.1, model.SeverityLow},
		{0, model.SeverityUnknown},
	}
	for _, tt := range tests {
		if got := scoreLabel(tt.score); got != tt.want {
			t.Errorf("scoreLabel(%v) = %q, want %q", tt.score, got, tt.want)
		}
	}
}
