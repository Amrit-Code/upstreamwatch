package report

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	// maxLineRunes caps every rendered headline and line.
	maxLineRunes = 300
	// maxSummaryRunes caps the advisory summary inside its line.
	maxSummaryRunes = 200
	// ellipsis ends a shortened line.
	ellipsis = "…"
)

// renderItem renders one finding as a digest item. changed marks an item of
// the CHANGED section. baseURL, when not empty, gives the item a link; dates
// are shown in loc. Every headline and line is sanitised.
func renderItem(d model.FindingDetail, changed bool, baseURL string, loc *time.Location) (model.DigestItem, error) {
	f := d.Finding
	item := model.DigestItem{FindingID: f.ID, Kind: f.Kind, Score: f.Score}
	var lines []string
	switch f.Kind {
	case model.KindAdvisory:
		item.Tag = "[A]"
		item.Headline = advisoryHeadline(d)
		lines = advisoryLines(d, loc)
		if changed {
			lines = append(lines, "Now: "+f.RankReason)
		}
	case model.KindJobFailure:
		item.Tag = "[J]"
		job, ok := strings.CutPrefix(f.Fingerprint, string(model.KindJobFailure)+"|")
		if !ok {
			job = f.Fingerprint
		}
		item.Headline = "job " + job + " failed"
		lines = []string{f.RankReason}
	case model.KindIssue:
		item.Tag = "[I]"
		item.Headline = f.RankReason
	default:
		return model.DigestItem{}, fmt.Errorf("render finding %d: unknown kind %q", f.ID, f.Kind)
	}

	item.Headline = sanitizeLine(item.Headline)
	for _, l := range lines {
		if l == "" {
			continue
		}
		item.Lines = append(item.Lines, sanitizeLine(l))
	}
	if baseURL != "" {
		item.URL = baseURL + "/f/" + strconv.FormatInt(f.ID, 10)
	}
	return item, nil
}

// advisoryHeadline is "<severity>  <advisory id>  <package URL> <version>
// (repo: <slug>)", with "unknown" for an empty severity and the version left
// out when the finding links no interval.
func advisoryHeadline(d model.FindingDetail) string {
	sev := string(d.Advisory.Severity)
	if sev == "" {
		sev = "unknown"
	}
	id := d.Advisory.ID
	if id == "" {
		id = d.Finding.AdvisoryID
	}
	pkg := d.Package.PURL
	if d.Interval.Version != "" {
		pkg += " " + d.Interval.Version
	}
	return sev + "  " + id + "  " + pkg + "  (repo: " + d.RepoSlug + ")"
}

// advisoryLines returns the summary-and-facts line and, when the advisory
// has a match, the exposure line.
func advisoryLines(d model.FindingDetail, loc *time.Location) []string {
	exp, hasExp := chooseExposure(d)

	summary := cleanUpstream(d.Advisory.Summary)
	if summary == "" {
		summary = firstSentence(cleanUpstream(d.Advisory.Details))
	}
	summary = truncateRunes(summary, maxSummaryRunes)

	var facts []string
	if hasExp && exp.Match.FixedVersion != "" {
		facts = append(facts, "Fixed in "+exp.Match.FixedVersion+".")
	}
	if d.Advisory.KEV {
		facts = append(facts, "KEV: yes.")
	} else {
		facts = append(facts, "KEV: no.")
	}
	if d.Advisory.HasEPSS {
		facts = append(facts, fmt.Sprintf("EPSS %.2f (p%d).", d.Advisory.EPSS, int(math.Round(d.Advisory.EPSSPercentile*100))))
	}
	first := strings.Join(facts, " ")
	if summary != "" {
		first = summary + " " + first
	}
	lines := []string{first}
	if hasExp {
		lines = append(lines, exposureLine(exp, loc))
	}
	return lines
}

// chooseExposure picks the match the exposure line describes: the linked
// interval's match when it is open, else the first open match, else the
// linked match, else the latest match. It returns false when there is none.
func chooseExposure(d model.FindingDetail) (model.Exposure, bool) {
	if len(d.Exposures) == 0 {
		return model.Exposure{}, false
	}
	linked := -1
	if d.Finding.DepVersionID != 0 {
		for i, e := range d.Exposures {
			if e.Interval.ID == d.Finding.DepVersionID {
				linked = i
				break
			}
		}
	}
	if linked >= 0 && d.Exposures[linked].Match.IsOpen() {
		return d.Exposures[linked], true
	}
	for _, e := range d.Exposures {
		if e.Match.IsOpen() {
			return e, true
		}
	}
	if linked >= 0 {
		return d.Exposures[linked], true
	}
	return d.Exposures[len(d.Exposures)-1], true
}

// exposureLine describes one match: "Exposed since <date> (<manifest>) —
// confidence <c>" for an open one, with "(pinned since <date>)" appended when
// the pin started before the exposure, or "Exposed <from> to <to> (<manifest>,
// no longer pinned) — confidence <c>" for a closed one.
func exposureLine(e model.Exposure, loc *time.Location) string {
	m, iv := e.Match, e.Interval
	if m.IsOpen() {
		line := "Exposed since " + day(m.ExposedFrom, loc) + " (" + iv.ManifestPath + ") — confidence " + string(m.Confidence)
		if !iv.From.Equal(m.ExposedFrom) {
			line += " (pinned since " + day(iv.From, loc) + ")"
		}
		return line
	}
	return "Exposed " + day(m.ExposedFrom, loc) + " to " + day(m.ExposedTo, loc) +
		" (" + iv.ManifestPath + ", no longer pinned) — confidence " + string(m.Confidence)
}

// day formats t as YYYY-MM-DD in loc.
func day(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(time.DateOnly)
}

// sanitizeLine makes s safe to show on one line: invalid UTF-8 is replaced,
// every control character (CR, LF and tab included) becomes a space, and the
// result is capped at maxLineRunes runes.
func sanitizeLine(s string) string {
	return truncateRunes(replaceControls(s), maxLineRunes)
}

// cleanUpstream normalises text from an upstream source: invalid UTF-8 is
// replaced, control characters become spaces, and runs of white space
// collapse to one space with none at either end.
func cleanUpstream(s string) string {
	return strings.Join(strings.Fields(replaceControls(s)), " ")
}

// replaceControls replaces invalid UTF-8 with U+FFFD and every control
// character with a space.
func replaceControls(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, string(utf8.RuneError)))
}

// firstSentence returns s up to and including the first ".", "!" or "?"
// that ends s or is followed by a space; all of s when there is none.
func firstSentence(s string) string {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.', '!', '?':
			if i+1 == len(s) || s[i+1] == ' ' {
				return s[:i+1]
			}
		}
	}
	return s
}

// truncateRunes returns s when it has at most n runes, and otherwise its
// first n-1 runes followed by "…".
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n-1 {
			return s[:i] + ellipsis
		}
		count++
	}
	return s
}
