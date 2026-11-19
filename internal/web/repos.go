package web

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
)

// shortSHALen is how many characters of a commit SHA the pages show.
const shortSHALen = 12

// reposPage is the view data of GET /repos.
type reposPage struct {
	Title string
	Repos []repoRow
}

// repoRow is one row of the repos table, already formatted.
type repoRow struct {
	Slug       string
	Link       string
	URL        string
	Branch     string
	Commit     string
	LastFetch  string
	FetchError string
}

// timelinePage is the view data of GET /repos/{slug}.
type timelinePage struct {
	Title    string
	Repo     repoRow
	Q        string // the q filter as submitted
	On       string // the on filter as submitted, YYYY-MM-DD or empty
	Package  string // the pkg filter; non-empty makes the page a package history
	ClearURL string // the page with no filters
	AllURL   string // back from a package history to all packages, keeping on
	Rows     []intervalRow
	Empty    string // the empty-state line shown when Rows is empty
}

// intervalRow is one row of the interval table, already formatted.
type intervalRow struct {
	Open       bool
	Package    string
	PackageURL string
	Version    string
	From       string
	To         string
	Direct     string
	Manifest   string
}

// errorPage is the view data of the short error pages.
type errorPage struct {
	Title   string
	Message string
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.timeline.Repos(r.Context())
	if err != nil {
		s.logger.Error("load repos failed", "error", err.Error())
		s.renderError(w, http.StatusInternalServerError)
		return
	}
	page := reposPage{Title: "Repositories", Repos: make([]repoRow, 0, len(repos))}
	for _, repo := range repos {
		page.Repos = append(page.Repos, newRepoRow(repo))
	}
	s.render(w, http.StatusOK, "repos", page)
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	params := r.URL.Query()
	q, on, pkg := params.Get("q"), params.Get("on"), params.Get("pkg")
	f := query.TimelineFilter{Package: pkg, Contains: q}
	if on != "" {
		day, err := time.Parse(time.DateOnly, on)
		if err != nil {
			s.render(w, http.StatusBadRequest, "error", errorPage{
				Title:   "Bad request",
				Message: "The on date must be a date in the form YYYY-MM-DD.",
			})
			return
		}
		// The last instant of that UTC day.
		f.On = day.AddDate(0, 0, 1).Add(-time.Nanosecond)
	}

	repo, intervals, err := s.timeline.Timeline(r.Context(), slug, f)
	if errors.Is(err, model.ErrNotFound) {
		s.render(w, http.StatusNotFound, "error", errorPage{
			Title:   "Not found",
			Message: "No repository with that name.",
		})
		return
	}
	if err != nil {
		s.logger.Error("load timeline failed", "repo", slug, "error", err.Error())
		s.renderError(w, http.StatusInternalServerError)
		return
	}

	clearURL := repoLink(repo.Slug)
	page := timelinePage{
		Title:    repo.Slug,
		Repo:     newRepoRow(repo),
		Q:        q,
		On:       on,
		Package:  pkg,
		ClearURL: clearURL,
		AllURL:   clearURL,
		Rows:     make([]intervalRow, 0, len(intervals)),
	}
	if on != "" {
		page.AllURL = clearURL + "?on=" + url.QueryEscape(on)
	}
	for _, iv := range intervals {
		page.Rows = append(page.Rows, newIntervalRow(iv, on))
	}
	if f == (query.TimelineFilter{}) {
		page.Empty = "Not indexed yet."
	} else {
		page.Empty = "No intervals match."
	}
	s.render(w, http.StatusOK, "timeline", page)
}

// renderError renders the generic error page for status; the detail stays in
// the log.
func (s *Server) renderError(w http.ResponseWriter, status int) {
	s.render(w, status, "error", errorPage{Title: "Error", Message: "Internal server error."})
}

func newRepoRow(repo model.TargetRepo) repoRow {
	row := repoRow{
		Slug:       repo.Slug,
		Link:       repoLink(repo.Slug),
		URL:        repo.URL,
		Branch:     repo.DefaultBranch,
		Commit:     shortSHA(repo.LastIndexedSHA),
		FetchError: repo.LastFetchError,
	}
	if !repo.LastFetchAt.IsZero() {
		row.LastFetch = repo.LastFetchAt.UTC().Format("2006-01-02 15:04")
	}
	return row
}

// newIntervalRow formats iv; on is the submitted on filter, kept in the
// package link.
func newIntervalRow(iv model.DepInterval, on string) intervalRow {
	row := intervalRow{
		Open:       iv.IsOpen(),
		Package:    iv.Package.PURL,
		PackageURL: "?pkg=" + url.QueryEscape(iv.Package.PURL),
		Version:    iv.Version,
		From:       iv.From.UTC().Format(time.DateOnly),
		To:         "now",
		Direct:     directLabel(iv.Direct),
		Manifest:   iv.ManifestPath,
	}
	if on != "" {
		row.PackageURL += "&on=" + url.QueryEscape(on)
	}
	if row.Version == "" && iv.Requirement != "" {
		row.Version = "(" + iv.Requirement + ")"
	}
	if !iv.IsOpen() {
		row.To = iv.To.UTC().Format(time.DateOnly)
	}
	return row
}

func directLabel(d model.Directness) string {
	switch d {
	case model.DirectYes:
		return "yes"
	case model.DirectNo:
		return "no"
	default:
		return "?"
	}
}

func repoLink(slug string) string { return "/repos/" + url.PathEscape(slug) }

func shortSHA(sha string) string {
	if len(sha) > shortSHALen {
		return sha[:shortSHALen]
	}
	return sha
}
