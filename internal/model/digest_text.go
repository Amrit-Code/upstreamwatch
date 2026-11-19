package model

import (
	"strconv"
	"strings"
	"time"
)

// digestIndent is the indentation of every line below an item's headline.
const digestIndent = "     "

// PlainText lays the digest out as plain text for email, ntfy and stdout.
//
// A digest with items has a header line with the counts, a NEW and a CHANGED
// section (each left out when it has nothing to report), a WAITING line when
// issue candidates are over the LLM budget, the last run of each job and the
// counts of unresolved findings by state. A heartbeat replaces the header and
// sections with an "all quiet" line. Dates and times are shown in
// GeneratedAt's location. Headline, Lines and URL are written as they are;
// the digest job sanitises them. The text ends with a newline.
func (d Digest) PlainText() string {
	var b strings.Builder
	if d.Heartbeat {
		b.WriteString(d.Subject())
		b.WriteString("\n\nNo new or changed findings since the last digest.\n\n")
	} else {
		b.WriteString(d.Subject())
		b.WriteString(", " + strconv.Itoa(d.Waiting) + " waiting for LLM budget\n\n")
		d.writeSection(&b, "NEW", d.New, d.MoreNew)
		d.writeSection(&b, "CHANGED", d.Changed, d.MoreChanged)
		if d.Waiting > 0 {
			b.WriteString("WAITING  " + strconv.Itoa(d.Waiting) + " issue candidates over today's LLM budget.\n")
		}
	}
	d.writeLastRuns(&b)
	b.WriteString("Open: " + strconv.Itoa(d.Open) + ", acked: " + strconv.Itoa(d.Acked) + ", snoozed: " + strconv.Itoa(d.Snoozed) + ".")
	if d.FindingsURL != "" {
		b.WriteString("  " + d.FindingsURL)
	}
	b.WriteString("\n")
	return b.String()
}

// Subject is the one-line summary used as an email subject and ntfy title.
func (d Digest) Subject() string {
	prefix := "upstreamwatch — " + d.GeneratedAt.Format(time.DateOnly) + " — "
	if d.Heartbeat {
		return prefix + "all quiet"
	}
	return prefix + strconv.Itoa(len(d.New)+d.MoreNew) + " new, " + strconv.Itoa(len(d.Changed)+d.MoreChanged) + " changed"
}

// writeSection writes one titled section followed by a blank line: its items
// separated by blank lines, then an "… and N more" line when more is
// positive. A section with no items and nothing more is left out.
func (d Digest) writeSection(b *strings.Builder, title string, items []DigestItem, more int) {
	if len(items) == 0 && more <= 0 {
		return
	}
	b.WriteString(title + "\n")
	for i, it := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(it.Tag + " " + it.Headline + "\n")
		for _, l := range it.Lines {
			b.WriteString(digestIndent + l + "\n")
		}
		if it.URL != "" {
			b.WriteString(digestIndent + it.URL + "\n")
		}
	}
	if more > 0 {
		if len(items) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(digestIndent + "… and " + strconv.Itoa(more) + " more")
		if d.FindingsURL != "" {
			b.WriteString(" " + d.FindingsURL)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// writeLastRuns writes the "Last runs:" line: each run as job, time and
// outcome, in the order LastRuns lists them.
func (d Digest) writeLastRuns(b *strings.Builder) {
	if len(d.LastRuns) == 0 {
		b.WriteString("Last runs: none.\n")
		return
	}
	loc := d.GeneratedAt.Location()
	parts := make([]string, 0, len(d.LastRuns))
	for _, r := range d.LastRuns {
		at := r.FinishedAt
		if r.Status == RunRunning || at.IsZero() {
			at = r.StartedAt
		}
		fields := []string{r.Job}
		// A run with no recorded time is shown without one rather than as
		// the zero time.
		if !at.IsZero() {
			fields = append(fields, at.In(loc).Format("15:04"))
		}
		if o := runOutcome(r.Status); o != "" {
			fields = append(fields, o)
		}
		parts = append(parts, strings.Join(fields, " "))
	}
	b.WriteString("Last runs: " + strings.Join(parts, ", ") + ".\n")
}

// runOutcome is the word the digest uses for a run status.
func runOutcome(s RunStatus) string {
	switch s {
	case RunSucceeded:
		return "ok"
	case RunFailed:
		return "failed"
	case RunRunning:
		return "running"
	default:
		return string(s)
	}
}
