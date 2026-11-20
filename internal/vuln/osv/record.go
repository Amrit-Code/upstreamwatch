package osv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// purlTypes are the package URL types the adapter maps OSV packages back to.
// model.OSVEcosystem stays the authority for which OSV ecosystem each one is;
// a type it does not know never matches. Read-only.
var purlTypes = []string{
	"golang", "pypi", "npm", "cargo", "gem", "nuget", "maven", "composer", "pub", "hex", "githubactions",
}

// ecosystemActions is the model.Package Ecosystem of a GitHub Action as the
// timeline stores it; it differs from the package URL type.
const ecosystemActions = "github-actions"

const (
	// recordMaxBytes caps the download of one OSV record.
	recordMaxBytes = 2 << 20
	// detailsMaxBytes caps an advisory's Details; a longer text is cut on a
	// rune boundary and ends with detailsEllipsis.
	detailsMaxBytes = 64 << 10
	detailsEllipsis = "…"
)

// osvRecord is the part of an OSV record upstreamwatch reads.
type osvRecord struct {
	ID       string   `json:"id"`
	Aliases  []string `json:"aliases"`
	Summary  string   `json:"summary"`
	Details  string   `json:"details"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	Published        string          `json:"published"`
	Modified         string          `json:"modified"`
	Withdrawn        string          `json:"withdrawn"`
	DatabaseSpecific json.RawMessage `json:"database_specific"`
	Affected         []osvAffected   `json:"affected"`
}

type osvAffected struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
		PURL      string `json:"purl"`
	} `json:"package"`
	Ranges []struct {
		Type   string `json:"type"`
		Repo   string `json:"repo"`
		Events []struct {
			Introduced   string `json:"introduced"`
			Fixed        string `json:"fixed"`
			LastAffected string `json:"last_affected"`
			Limit        string `json:"limit"`
		} `json:"events"`
	} `json:"ranges"`
	Versions          []string        `json:"versions"`
	EcosystemSpecific json.RawMessage `json:"ecosystem_specific"`
	DatabaseSpecific  json.RawMessage `json:"database_specific"`
}

// Fetch returns the full record for id, with its affected packages mapped to
// upstreamwatch's package URLs and its severity derived. An unknown id is an
// error wrapping model.ErrNotFound. A response over recordMaxBytes, or a
// record whose id is not id, is an error.
func (c *Client) Fetch(ctx context.Context, id string) (model.Advisory, error) {
	host := hostOf(c.base)
	resp, err := c.hc.Do(ctx, httpx.Request{
		Method:       http.MethodGet,
		URL:          c.base + "/v1/vulns/" + url.PathEscape(id),
		Validators:   httpx.ValidatorsNone,
		MaxBodyBytes: recordMaxBytes,
	})
	if se, ok := errors.AsType[*httpx.StatusError](err); ok && se.Status == http.StatusNotFound {
		return model.Advisory{}, fmt.Errorf("osv: fetch %s: %s: %w", id, host, model.ErrNotFound)
	}
	if err != nil {
		return model.Advisory{}, fmt.Errorf("osv: fetch %s: %s: %w", id, host, err)
	}
	a, err := parseRecord(resp.Body)
	if err != nil {
		return model.Advisory{}, fmt.Errorf("osv: fetch %s: %s: %w", id, host, err)
	}
	if a.ID != id {
		// The served id is upstream text, so it stays out of the error.
		return model.Advisory{}, fmt.Errorf("osv: fetch %s: %s: record id differs from the id requested", id, host)
	}
	return a, nil
}

// parseRecord converts an OSV record into an Advisory whose Raw is body.
// Details longer than detailsMaxBytes is cut; Raw keeps the exact body.
func parseRecord(body []byte) (model.Advisory, error) {
	var r osvRecord
	if err := json.Unmarshal(body, &r); err != nil {
		return model.Advisory{}, fmt.Errorf("decode record: %w", err)
	}
	if r.ID == "" {
		return model.Advisory{}, errors.New("record has no id")
	}
	a := model.Advisory{
		ID:      r.ID,
		Aliases: r.Aliases,
		Summary: r.Summary,
		Details: capDetails(r.Details),
		Raw:     body,
	}
	var err error
	if a.Modified, err = parseTime(r.Modified); err != nil {
		return model.Advisory{}, fmt.Errorf("modified: %w", err)
	}
	if a.Published, err = parseOptionalTime(r.Published); err != nil {
		return model.Advisory{}, fmt.Errorf("published: %w", err)
	}
	if a.Withdrawn, err = parseOptionalTime(r.Withdrawn); err != nil {
		return model.Advisory{}, fmt.Errorf("withdrawn: %w", err)
	}

	for _, s := range r.Severity {
		if s.Type != "CVSS_V3" {
			continue
		}
		// Only the first CVSS_V3 entry counts; a malformed one leaves the
		// score unset.
		if score, ok := cvssV3BaseScore(s.Score); ok {
			a.CVSSScore = score
			a.CVSSVector = s.Score
		}
		break
	}
	a.Severity = databaseSeverity(r.DatabaseSpecific)
	if a.Severity == model.SeverityUnknown && a.CVSSVector != "" {
		a.Severity = scoreLabel(a.CVSSScore)
	}

	sourceDB := model.SourceDB(r.ID)
	for _, e := range r.Affected {
		pkg, ok := toPackage(e.Package.Ecosystem, e.Package.Name)
		if !ok {
			continue
		}
		ap := model.AffectedPackage{
			Package:           pkg,
			Versions:          e.Versions,
			EcosystemSpecific: rawOrNil(e.EcosystemSpecific),
			DatabaseSpecific:  rawOrNil(e.DatabaseSpecific),
			SourceDB:          sourceDB,
		}
		for _, rg := range e.Ranges {
			ar := model.AffectedRange{Type: model.RangeType(rg.Type), Repo: rg.Repo}
			for _, ev := range rg.Events {
				ar.Events = append(ar.Events, model.RangeEvent{
					Introduced:   ev.Introduced,
					Fixed:        ev.Fixed,
					LastAffected: ev.LastAffected,
					Limit:        ev.Limit,
				})
			}
			ap.Ranges = append(ap.Ranges, ar)
		}
		a.Affected = append(a.Affected, ap)
	}
	return a, nil
}

// capDetails returns s when it is at most detailsMaxBytes long, and
// otherwise its longest prefix of whole runes within detailsMaxBytes
// followed by detailsEllipsis.
func capDetails(s string) string {
	if len(s) <= detailsMaxBytes {
		return s
	}
	cut := detailsMaxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + detailsEllipsis
}

// parseOptionalTime is parseTime, with an empty string meaning "not set".
func parseOptionalTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return parseTime(s)
}

// rawOrNil returns raw, or nil when the field was absent or JSON null.
func rawOrNil(raw json.RawMessage) []byte {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	return raw
}

// databaseSeverity returns the severity a record's top-level
// database_specific.severity names (GitHub's LOW, MODERATE, HIGH, CRITICAL;
// MEDIUM is accepted for MODERATE), or SeverityUnknown when there is none.
func databaseSeverity(raw json.RawMessage) model.SeverityLabel {
	if len(raw) == 0 {
		return model.SeverityUnknown
	}
	var fields struct {
		Severity json.RawMessage `json:"severity"`
	}
	var label string
	// database_specific is free-form: a value that is not an object, or a
	// severity that is not a string, means no usable severity, not an error.
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(fields.Severity, &label) != nil {
		return model.SeverityUnknown
	}
	switch strings.ToUpper(label) {
	case "CRITICAL":
		return model.SeverityCritical
	case "HIGH":
		return model.SeverityHigh
	case "MODERATE", "MEDIUM":
		return model.SeverityMedium
	case "LOW":
		return model.SeverityLow
	}
	return model.SeverityUnknown
}

// scoreLabel labels a CVSS base score: 9.0 and up critical, 7.0 high, 4.0
// medium, 0.1 low; 0.0 has no label.
func scoreLabel(score float64) model.SeverityLabel {
	switch {
	case score >= 9.0:
		return model.SeverityCritical
	case score >= 7.0:
		return model.SeverityHigh
	case score >= 4.0:
		return model.SeverityMedium
	case score >= 0.1:
		return model.SeverityLow
	}
	return model.SeverityUnknown
}

// toPackage maps an OSV ecosystem and package name to upstreamwatch's
// package. It reports false for an ecosystem with no package URL type in
// model.OSVEcosystem (Debian, Alpine and so on), for an empty name, and for a
// Maven name that is not "<group>:<artifact>". An ecosystem suffix such as
// ":12" in "Debian:12" is ignored.
func toPackage(ecosystem, name string) (model.Package, bool) {
	eco, _, _ := strings.Cut(ecosystem, ":")
	typ, ok := purlTypeOf(eco)
	if !ok || name == "" {
		return model.Package{}, false
	}
	switch typ {
	case "golang":
		if name == "stdlib" || name == "toolchain" {
			return model.Package{PURL: toolchainPURL, Ecosystem: typ, Name: "toolchain"}, true
		}
		return model.Package{PURL: "pkg:golang/" + name, Ecosystem: typ, Name: name}, true
	case "pypi":
		n := normalizePyPI(name)
		return model.Package{PURL: "pkg:pypi/" + n, Ecosystem: typ, Name: n}, true
	case "githubactions":
		return model.Package{PURL: "pkg:githubactions/" + name, Ecosystem: ecosystemActions, Name: name}, true
	case "npm":
		purlName := name
		if rest, ok := strings.CutPrefix(name, "@"); ok {
			purlName = "%40" + rest
		}
		return model.Package{PURL: "pkg:npm/" + purlName, Ecosystem: typ, Name: name}, true
	case "maven":
		group, artifact, ok := strings.Cut(name, ":")
		if !ok || group == "" || artifact == "" || strings.Contains(artifact, ":") {
			return model.Package{}, false
		}
		path := group + "/" + artifact
		return model.Package{PURL: "pkg:maven/" + path, Ecosystem: typ, Name: path}, true
	}
	return model.Package{PURL: "pkg:" + typ + "/" + name, Ecosystem: typ, Name: name}, true
}

// purlTypeOf returns the package URL type whose OSV ecosystem is eco.
func purlTypeOf(eco string) (string, bool) {
	for _, t := range purlTypes {
		if e, ok := model.OSVEcosystem("pkg:" + t + "/"); ok && e == eco {
			return t, true
		}
	}
	return "", false
}

// normalizePyPI applies the PEP 503 name normalisation: lower case, with
// every run of "-", "_" and "." replaced by one "-".
func normalizePyPI(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	sep := false
	for _, r := range strings.ToLower(name) {
		if r == '-' || r == '_' || r == '.' {
			sep = true
			continue
		}
		if sep {
			b.WriteByte('-')
			sep = false
		}
		b.WriteRune(r)
	}
	if sep {
		b.WriteByte('-')
	}
	return b.String()
}
