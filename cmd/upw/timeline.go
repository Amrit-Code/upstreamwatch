package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
)

const timelineSynopsis = `usage: upw timeline <slug> [package-url] [-on YYYY-MM-DD] [-json] [-config path] [-log-level level]

Print the dependency intervals of one indexed repo. A package URL such as
pkg:golang/github.com/gin-gonic/gin keeps only that package; -on keeps only
the intervals in force at the end of that UTC day.
`

// cmdTimeline prints a repo's dependency intervals from the store, as a table
// or as JSON. It builds no Runner and starts nothing.
func cmdTimeline(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) (err error) {
	c := newCommand("timeline", timelineSynopsis, stdout, stderr)
	var on time.Time
	c.fs.Func("on", "keep the intervals in force at the end of this UTC `date` (YYYY-MM-DD)", func(s string) error {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return errors.New("want a date as YYYY-MM-DD")
		}
		on = d.Add(24*time.Hour - time.Nanosecond)
		return nil
	})
	asJSON := c.fs.Bool("json", false, "print a JSON array instead of a table")

	// flag stops at the first non-flag argument, so the slug and the package
	// URL come off the front before the flags are parsed.
	if len(args) > 0 && (args[0] == "-h" || args[0] == "-help" || args[0] == "--help") {
		c.usage(stdout)
		return nil
	}
	if len(args) == 0 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return c.usageError("missing repo slug")
	}
	slug, rest := args[0], args[1:]
	var filter query.TimelineFilter
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		filter.Package, rest = rest[0], rest[1:]
	}
	if err := c.parse(rest); err != nil {
		return err
	}
	filter.On = on

	a, err := openApp(ctx, c.configPath, c.logger(), getenv)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.close()) }()

	q := query.NewService(a.db, a.db, a.db, a.logger)
	_, intervals, err := q.Timeline(ctx, slug, filter)
	switch {
	case errors.Is(err, model.ErrNotFound):
		return fmt.Errorf("unknown repo %q", slug)
	case err != nil:
		return err
	}
	if *asJSON {
		return writeTimelineJSON(stdout, intervals)
	}
	return writeTimelineText(stdout, intervals)
}

// writeTimelineText prints a header and one aligned line per interval, or
// "no intervals" when there are none.
func writeTimelineText(w io.Writer, intervals []model.DepInterval) error {
	if len(intervals) == 0 {
		_, err := fmt.Fprintln(w, "no intervals")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PACKAGE\tVERSION\tFROM\tTO\tDIRECT\tMANIFEST")
	for _, iv := range intervals {
		version := iv.Version
		if version == "" {
			version = "(" + iv.Requirement + ")"
		}
		to := "now"
		if !iv.IsOpen() {
			to = iv.To.UTC().Format(time.DateOnly)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			terminalSafe(iv.Package.PURL), terminalSafe(version), iv.From.UTC().Format(time.DateOnly), to,
			directText(iv.Direct), terminalSafe(iv.ManifestPath))
	}
	// Write errors on tw are kept by the tabwriter and returned by Flush.
	return tw.Flush()
}

// terminalSafe returns s, or its strconv.Quote form when s holds a control
// character (C0, DEL or C1) or is not valid UTF-8, so repository data cannot
// move the cursor, clear the screen or break the table's columns.
func terminalSafe(s string) string { return quoteUnsafe(s, "") }

// terminalSafeText is terminalSafe for message text such as an error or a
// stats line: a newline or a tab alone does not cause quoting, so an error
// that joins several problems still prints one per line.
func terminalSafeText(s string) string { return quoteUnsafe(s, "\n\t") }

// quoteUnsafe returns s, or its strconv.Quote form when s is not valid UTF-8
// or holds a control character that is not in keep.
func quoteUnsafe(s, keep string) string {
	unsafe := func(r rune) bool { return unicode.IsControl(r) && !strings.ContainsRune(keep, r) }
	if !utf8.ValidString(s) || strings.ContainsFunc(s, unsafe) {
		return strconv.Quote(s)
	}
	return s
}

// directText is the DIRECT column: yes, no, or ? when unknown.
func directText(d model.Directness) string {
	switch d {
	case model.DirectYes:
		return "yes"
	case model.DirectNo:
		return "no"
	default:
		return "?"
	}
}

// intervalJSON is one interval in the -json output. The field order is the
// key order of the output.
type intervalJSON struct {
	Package     string  `json:"package"`
	Ecosystem   string  `json:"ecosystem"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Requirement string  `json:"requirement"`
	Direct      *bool   `json:"direct"`
	Manifest    string  `json:"manifest"`
	From        string  `json:"from"`
	To          *string `json:"to"`
	FromSHA     string  `json:"from_sha"`
	ToSHA       *string `json:"to_sha"`
}

// writeTimelineJSON prints the intervals as a JSON array indented with two
// spaces, [] when there are none. Times are RFC 3339 in UTC; direct, to and
// to_sha are null when unknown or open.
func writeTimelineJSON(w io.Writer, intervals []model.DepInterval) error {
	rows := make([]intervalJSON, 0, len(intervals))
	for _, iv := range intervals {
		row := intervalJSON{
			Package:     iv.Package.PURL,
			Ecosystem:   iv.Package.Ecosystem,
			Name:        iv.Package.Name,
			Version:     iv.Version,
			Requirement: iv.Requirement,
			Manifest:    iv.ManifestPath,
			From:        iv.From.UTC().Format(time.RFC3339),
			FromSHA:     iv.FromSHA,
		}
		switch iv.Direct {
		case model.DirectYes:
			row.Direct = new(true)
		case model.DirectNo:
			row.Direct = new(false)
		}
		if !iv.IsOpen() {
			row.To = new(iv.To.UTC().Format(time.RFC3339))
			row.ToSHA = new(iv.ToSHA)
		}
		rows = append(rows, row)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Requirements such as ">=1.2" stay readable instead of >=1.2.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rows); err != nil {
		return fmt.Errorf("write timeline json: %w", err)
	}
	return nil
}
