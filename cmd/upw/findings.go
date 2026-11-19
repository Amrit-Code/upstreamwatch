package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
)

const findingsSynopsis = `usage: upw findings [-json] [-repo slug] [-suppressed] [-config path] [-log-level level]

Print the unresolved findings, highest score first: by default those whose
state is open or acked, with -suppressed also the snoozed and dismissed ones.
-repo keeps one target's findings only.
`

// cmdFindings prints the ranked findings from the store, as a table or as
// JSON. It builds no Runner and starts nothing.
func cmdFindings(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) (err error) {
	c := newCommand("findings", findingsSynopsis, stdout, stderr)
	asJSON := c.fs.Bool("json", false, "print a JSON array instead of a table")
	repo := c.fs.String("repo", "", "keep the findings of the target with this `slug`")
	suppressed := c.fs.Bool("suppressed", false, "also print snoozed and dismissed findings")
	if err := c.parse(args); err != nil {
		return err
	}

	a, err := openApp(ctx, c.configPath, c.logger(), getenv)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.close()) }()

	q := query.NewService(a.db, a.db, a.db, a.logger)
	now := time.Now().UTC()
	filter := model.FindingFilter{Suppressed: *suppressed, Now: now}
	if *repo != "" {
		repos, err := q.Repos(ctx)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(repos, func(r model.TargetRepo) bool { return r.Slug == *repo })
		if i < 0 {
			return fmt.Errorf("unknown repo %q", *repo)
		}
		filter.RepoID = repos[i].ID
	}
	details, err := q.Findings(ctx, filter)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeFindingsJSON(stdout, details, now, strings.TrimRight(a.cfg.HTTP.BaseURL, "/"))
	}
	return writeFindingsText(stdout, details, now)
}

// findingRow is the printed fields of one finding, shared by the table and
// the JSON output.
type findingRow struct {
	ref        string // the advisory ID, or the job name of a job_failure
	advisoryID string
	version    string
	severity   string
	confidence string
	exposedNow bool
	state      string
}

// newFindingRow derives the printed fields of d. State is the effective
// state at now, so an expired snooze shows as open.
func newFindingRow(d model.FindingDetail, now time.Time) findingRow {
	f := d.Finding
	row := findingRow{state: string(f.State.Effective(now))}
	if f.DepVersionID != 0 {
		row.version = d.Interval.Version
		if row.version == "" && d.Interval.Requirement != "" {
			row.version = "(" + d.Interval.Requirement + ")"
		}
	}
	switch f.Kind {
	case model.KindAdvisory:
		row.ref, row.advisoryID = f.AdvisoryID, f.AdvisoryID
		row.severity = string(d.Advisory.Severity)
		if row.severity == "" {
			row.severity = "unknown"
		}
		for _, e := range d.Exposures {
			if e.Match.DepVersionID == f.DepVersionID && f.DepVersionID != 0 {
				row.confidence = string(e.Match.Confidence)
			}
			row.exposedNow = row.exposedNow || e.Match.IsOpen()
		}
	case model.KindJobFailure:
		row.ref, _ = strings.CutPrefix(f.Fingerprint, string(model.KindJobFailure)+"|")
	}
	return row
}

// writeFindingsText prints a header and one aligned line per finding, or
// "no findings" when there are none. Every field taken from stored data is
// printed through terminalSafe.
func writeFindingsText(w io.Writer, details []model.FindingDetail, now time.Time) error {
	if len(details) == 0 {
		_, err := fmt.Fprintln(w, "no findings")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SCORE\tKIND\tREPO\tPACKAGE\tVERSION\tID\tSEVERITY\tCONFIDENCE\tSTATE")
	for _, d := range details {
		row := newFindingRow(d, now)
		fmt.Fprintf(tw, "%.1f\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			d.Finding.Score, terminalSafe(string(d.Finding.Kind)), terminalSafe(d.RepoSlug), terminalSafe(d.Package.PURL),
			terminalSafe(row.version), terminalSafe(row.ref), terminalSafe(row.severity), terminalSafe(row.confidence),
			terminalSafe(row.state))
	}
	// Write errors on tw are kept by the tabwriter and returned by Flush.
	return tw.Flush()
}

// findingJSON is one finding in the -json output. The field order is the
// key order of the output.
type findingJSON struct {
	ID          int64   `json:"id"`
	Kind        string  `json:"kind"`
	Score       float64 `json:"score"`
	RankReason  string  `json:"rank_reason"`
	Repo        string  `json:"repo"`
	Package     string  `json:"package"`
	Version     string  `json:"version"`
	AdvisoryID  string  `json:"advisory_id"`
	Severity    string  `json:"severity"`
	Confidence  string  `json:"confidence"`
	ExposedNow  bool    `json:"exposed_now"`
	State       string  `json:"state"`
	FirstSeen   string  `json:"first_seen"`
	LastChanged string  `json:"last_changed"`
	URL         string  `json:"url,omitempty"`
}

// writeFindingsJSON prints the findings as a JSON array indented with two
// spaces, [] when there are none. Times are RFC 3339 in UTC. url is baseURL
// (no trailing slash) followed by /f/<id>, and left out when baseURL is
// empty.
func writeFindingsJSON(w io.Writer, details []model.FindingDetail, now time.Time, baseURL string) error {
	rows := make([]findingJSON, 0, len(details))
	for _, d := range details {
		f := d.Finding
		row := newFindingRow(d, now)
		out := findingJSON{
			ID:          f.ID,
			Kind:        string(f.Kind),
			Score:       f.Score,
			RankReason:  f.RankReason,
			Repo:        d.RepoSlug,
			Package:     d.Package.PURL,
			Version:     row.version,
			AdvisoryID:  row.advisoryID,
			Severity:    row.severity,
			Confidence:  row.confidence,
			ExposedNow:  row.exposedNow,
			State:       row.state,
			FirstSeen:   f.FirstSeenAt.UTC().Format(time.RFC3339),
			LastChanged: f.LastChangedAt.UTC().Format(time.RFC3339),
		}
		if baseURL != "" {
			out.URL = baseURL + "/f/" + strconv.FormatInt(f.ID, 10)
		}
		rows = append(rows, out)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Package URLs and requirements such as ">=1.2" stay readable.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rows); err != nil {
		return fmt.Errorf("write findings json: %w", err)
	}
	return nil
}
