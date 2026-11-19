package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// maxFormBytes caps the body of a finding form: a note and a snooze length.
const maxFormBytes = 16 << 10

// webActor is who the finding state records as having made a change from the
// web UI.
const webActor = "web"

// dateTimeLayout is how the pages show a time of day, in UTC.
const dateTimeLayout = "2006-01-02 15:04"

// snoozeLengths are the values of the snooze form's for field.
var snoozeLengths = map[string]time.Duration{
	"1d":  24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// errBadSnoozeLength reports a snooze form whose for field is not one of
// snoozeLengths.
var errBadSnoozeLength = errors.New("snooze length is not 1d, 7d or 30d")

// findingsPage is the view data of GET /.
type findingsPage struct {
	Title      string
	Repos      []filterOption
	Kinds      []filterOption
	Suppressed bool
	Rows       []findingListRow
	Empty      string // the empty-state line shown when Rows is empty
}

// filterOption is one option of a filter select.
type filterOption struct {
	Value    string
	Selected bool
}

// findingListRow is one row of the findings table, already formatted.
type findingListRow struct {
	Link       string
	Score      string
	Kind       string
	Repo       string
	Package    string
	Version    string
	Ref        string // the advisory ID, or the job name of a job_failure
	Severity   string
	Confidence string
	State      string
	FirstSeen  string
}

// findingPage is the view data of GET /f/{id}.
type findingPage struct {
	Title      string
	Base       string // /f/{id}, the prefix of the form actions
	Kind       string
	Score      string
	RankReason string

	State        string
	SnoozedUntil string // set only while the snooze runs
	Note         string
	ChangedAt    string
	ChangedBy    string
	Reopen       bool // the effective state is not open

	RepoSlug string
	RepoLink string
	Package  string
	Interval bool // the finding links to an interval
	Version  string
	Manifest string

	Advisory *advisoryView // nil unless the finding is an advisory
}

// advisoryView is the advisory section of the finding page, already
// formatted.
type advisoryView struct {
	ID        string
	Aliases   string
	Summary   string
	Details   string
	Severity  string
	CVSS      string
	Published string
	Modified  string
	KEV       string
	EPSS      string
	Exposures []exposureRow
	// PinNote is true when some exposure starts later than its pin.
	PinNote bool
}

// exposureRow is one row of the exposures table, already formatted.
type exposureRow struct {
	Open        bool
	Version     string
	Manifest    string
	ExposedFrom string
	PinnedFrom  string // set only when it is a different day from ExposedFrom
	ExposedTo   string
	Fixed       string
	Confidence  string
	Reason      string
}

func (s *Server) handleFindings(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	slug, kind := params.Get("repo"), params.Get("kind")
	f := model.FindingFilter{Suppressed: params.Get("suppressed") == "1"}

	switch model.FindingKind(kind) {
	case "":
	case model.KindAdvisory, model.KindIssue, model.KindJobFailure:
		f.Kind = model.FindingKind(kind)
	default:
		s.renderBadRequest(w, "The kind must be advisory, issue or job_failure.")
		return
	}

	repos, err := s.timeline.Repos(r.Context())
	if err != nil {
		s.logger.Error("load repos failed", "error", err.Error())
		s.renderError(w, http.StatusInternalServerError)
		return
	}
	page := findingsPage{Title: "Findings", Suppressed: f.Suppressed}
	for _, repo := range repos {
		page.Repos = append(page.Repos, filterOption{Value: repo.Slug, Selected: repo.Slug == slug})
		if repo.Slug == slug {
			f.RepoID = repo.ID
		}
	}
	if slug != "" && f.RepoID == 0 {
		s.renderBadRequest(w, "No repository with that name.")
		return
	}
	for _, k := range []model.FindingKind{model.KindAdvisory, model.KindIssue, model.KindJobFailure} {
		page.Kinds = append(page.Kinds, filterOption{Value: string(k), Selected: k == f.Kind})
	}

	details, err := s.findings.Findings(r.Context(), f)
	if err != nil {
		s.logger.Error("load findings failed", "error", err.Error())
		s.renderError(w, http.StatusInternalServerError)
		return
	}
	now := s.now()
	page.Rows = make([]findingListRow, 0, len(details))
	for _, d := range details {
		page.Rows = append(page.Rows, newFindingListRow(d, now))
	}
	page.Empty = "No open findings."
	if f.Suppressed {
		page.Empty = "No findings."
	}
	s.render(w, http.StatusOK, "findings", page)
}

func (s *Server) handleFinding(w http.ResponseWriter, r *http.Request) {
	id, ok := findingID(r)
	if !ok {
		s.renderFindingNotFound(w)
		return
	}
	d, err := s.findings.FindingDetail(r.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		s.renderFindingNotFound(w)
		return
	}
	if err != nil {
		s.logger.Error("load finding failed", "finding_id", id, "error", err.Error())
		s.renderError(w, http.StatusInternalServerError)
		return
	}
	s.render(w, http.StatusOK, "finding", newFindingPage(d, s.now()))
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	s.changeState(w, r, func(ctx context.Context, id int64, note string) error {
		_, err := s.actions.Ack(ctx, id, note, webActor)
		return err
	})
}

func (s *Server) handleSnooze(w http.ResponseWriter, r *http.Request) {
	s.changeState(w, r, func(ctx context.Context, id int64, note string) error {
		d, ok := snoozeLengths[r.PostFormValue("for")]
		if !ok {
			return errBadSnoozeLength
		}
		_, err := s.actions.Snooze(ctx, id, d, note, webActor)
		return err
	})
}

func (s *Server) handleDismiss(w http.ResponseWriter, r *http.Request) {
	s.changeState(w, r, func(ctx context.Context, id int64, note string) error {
		_, err := s.actions.Dismiss(ctx, id, note, webActor)
		return err
	})
}

func (s *Server) handleReopen(w http.ResponseWriter, r *http.Request) {
	s.changeState(w, r, func(ctx context.Context, id int64, _ string) error {
		_, err := s.actions.Reopen(ctx, id, webActor)
		return err
	})
}

// changeState parses the finding ID and the form, calls act with the ID and
// the note (line endings normalised to \n) and answers: 303 to the finding
// page on success, 404 for an unknown ID, 400 for a change the finding does
// not allow or a bad snooze length, 413 for a body over maxFormBytes.
func (s *Server) changeState(w http.ResponseWriter, r *http.Request, act func(ctx context.Context, id int64, note string) error) {
	id, ok := findingID(r)
	if !ok {
		s.renderFindingNotFound(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
			s.render(w, http.StatusRequestEntityTooLarge, "error", errorPage{
				Title:   "Request too large",
				Message: "The form is larger than 16 KiB.",
			})
			return
		}
		s.renderBadRequest(w, "The form could not be read.")
		return
	}

	err := act(r.Context(), id, normaliseNote(r.PostFormValue("note")))
	switch {
	case err == nil:
		http.Redirect(w, r, findingLink(id), http.StatusSeeOther)
	case errors.Is(err, errBadSnoozeLength):
		s.renderBadRequest(w, "The snooze length must be 1d, 7d or 30d.")
	case errors.Is(err, model.ErrNotFound):
		s.renderFindingNotFound(w)
	case errors.Is(err, model.ErrInvalidTransition):
		s.renderBadRequest(w, "The finding was not changed: "+err.Error())
	default:
		s.logger.Error("change finding state failed", "finding_id", id, "error", err.Error())
		s.renderError(w, http.StatusInternalServerError)
	}
}

// renderBadRequest renders the error page with status 400 and message.
func (s *Server) renderBadRequest(w http.ResponseWriter, message string) {
	s.render(w, http.StatusBadRequest, "error", errorPage{Title: "Bad request", Message: message})
}

func (s *Server) renderFindingNotFound(w http.ResponseWriter) {
	s.render(w, http.StatusNotFound, "error", errorPage{Title: "Not found", Message: "No finding with that ID."})
}

// findingID returns the {id} path value as a number, and false when it is
// not one.
func findingID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

// normaliseNote turns the CRLF and lone CR line endings browsers send into LF.
func normaliseNote(note string) string {
	return strings.ReplaceAll(strings.ReplaceAll(note, "\r\n", "\n"), "\r", "\n")
}

func findingLink(id int64) string { return "/f/" + strconv.FormatInt(id, 10) }

func newFindingListRow(d model.FindingDetail, now time.Time) findingListRow {
	f := d.Finding
	row := findingListRow{
		Link:      findingLink(f.ID),
		Score:     formatScore(f.Score),
		Kind:      string(f.Kind),
		Repo:      d.RepoSlug,
		Package:   d.Package.PURL,
		Version:   intervalVersion(d.Interval),
		State:     string(f.State.Effective(now)),
		FirstSeen: f.FirstSeenAt.UTC().Format(dateTimeLayout),
	}
	switch f.Kind {
	case model.KindAdvisory:
		row.Ref = f.AdvisoryID
		row.Severity = severityLabel(d.Advisory.Severity)
		if m, ok := linkedMatch(d); ok {
			row.Confidence = string(m.Confidence)
		}
	case model.KindJobFailure:
		row.Ref = jobName(f)
	}
	return row
}

func newFindingPage(d model.FindingDetail, now time.Time) findingPage {
	f := d.Finding
	state := f.State.Effective(now)
	page := findingPage{
		Title:      "Finding " + strconv.FormatInt(f.ID, 10),
		Base:       findingLink(f.ID),
		Kind:       string(f.Kind),
		Score:      formatScore(f.Score),
		RankReason: f.RankReason,
		State:      string(state),
		Note:       f.State.Note,
		ChangedBy:  f.State.ChangedBy,
		Reopen:     state != model.StateOpen,
		RepoSlug:   d.RepoSlug,
		Package:    d.Package.PURL,
	}
	if state == model.StateSnoozed {
		page.SnoozedUntil = f.State.SnoozeUntil.UTC().Format(dateTimeLayout)
	}
	if !f.State.ChangedAt.IsZero() {
		page.ChangedAt = f.State.ChangedAt.UTC().Format(dateTimeLayout)
	}
	if d.RepoSlug != "" {
		page.RepoLink = repoLink(d.RepoSlug)
	}
	if f.DepVersionID != 0 {
		page.Interval = true
		page.Version = intervalVersion(d.Interval)
		page.Manifest = d.Interval.ManifestPath
		if page.Package == "" {
			page.Package = d.Interval.Package.PURL
		}
	}
	if f.Kind == model.KindAdvisory {
		page.Advisory = newAdvisoryView(d)
	}
	return page
}

func newAdvisoryView(d model.FindingDetail) *advisoryView {
	a := d.Advisory
	v := &advisoryView{
		ID:        a.ID,
		Aliases:   strings.Join(a.Aliases, ", "),
		Summary:   a.Summary,
		Details:   a.Details,
		Severity:  severityLabel(a.Severity),
		CVSS:      "none",
		Published: formatDate(a.Published),
		Modified:  formatDate(a.Modified),
		KEV:       "no",
		EPSS:      "not known",
		Exposures: make([]exposureRow, 0, len(d.Exposures)),
	}
	if a.ID == "" {
		v.ID = d.Finding.AdvisoryID
	}
	if a.CVSSVector != "" {
		v.CVSS = fmt.Sprintf("%.1f (%s)", a.CVSSScore, a.CVSSVector)
	}
	if a.KEV {
		v.KEV = "yes"
		if !a.KEVAddedAt.IsZero() {
			v.KEV = "yes, added " + formatDate(a.KEVAddedAt)
		}
	}
	if a.HasEPSS {
		v.EPSS = fmt.Sprintf("score %.4f, percentile %.4f", a.EPSS, a.EPSSPercentile)
	}
	for _, e := range d.Exposures {
		row := exposureRow{
			Open:        e.Match.IsOpen(),
			Version:     intervalVersion(e.Interval),
			Manifest:    e.Interval.ManifestPath,
			ExposedFrom: formatDate(e.Match.ExposedFrom),
			ExposedTo:   "still exposed",
			Fixed:       e.Match.FixedVersion,
			Confidence:  string(e.Match.Confidence),
			Reason:      e.Match.ConfidenceReason,
		}
		if pinned := formatDate(e.Interval.From); pinned != row.ExposedFrom {
			row.PinnedFrom = pinned
			v.PinNote = true
		}
		if !e.Match.IsOpen() {
			row.ExposedTo = formatDate(e.Match.ExposedTo)
		}
		v.Exposures = append(v.Exposures, row)
	}
	return v
}

// linkedMatch returns the exposure match of the interval the finding links
// to, and false when there is none.
func linkedMatch(d model.FindingDetail) (model.AdvisoryMatch, bool) {
	if d.Finding.DepVersionID == 0 {
		return model.AdvisoryMatch{}, false
	}
	for _, e := range d.Exposures {
		if e.Match.DepVersionID == d.Finding.DepVersionID {
			return e.Match, true
		}
	}
	return model.AdvisoryMatch{}, false
}

// jobName returns the job of a job_failure finding, taken from its
// fingerprint "job_failure|<job>".
func jobName(f model.Finding) string {
	name, _ := strings.CutPrefix(f.Fingerprint, string(model.KindJobFailure)+"|")
	return name
}

// intervalVersion is the interval's resolved version, or its requirement in
// parentheses when it has none.
func intervalVersion(iv model.DepInterval) string {
	if iv.Version == "" && iv.Requirement != "" {
		return "(" + iv.Requirement + ")"
	}
	return iv.Version
}

func severityLabel(s model.SeverityLabel) string {
	if s == model.SeverityUnknown {
		return "unknown"
	}
	return string(s)
}

func formatScore(score float64) string { return fmt.Sprintf("%.1f", score) }

// formatDate is t's UTC date, or "unknown" for the zero time.
func formatDate(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format(time.DateOnly)
}
