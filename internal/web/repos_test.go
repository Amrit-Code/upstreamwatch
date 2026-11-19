package web_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
)

const sha1 = "0123456789abcdef0123456789abcdef01234567"

var (
	pkgGin   = model.Package{PURL: "pkg:golang/github.com/gin-gonic/gin", Ecosystem: "golang", Name: "github.com/gin-gonic/gin"}
	pkgToml  = model.Package{PURL: "pkg:golang/github.com/BurntSushi/toml", Ecosystem: "golang", Name: "github.com/BurntSushi/toml"}
	pkgImage = model.Package{PURL: "pkg:docker/library/alpine", Ecosystem: "docker", Name: "library/alpine"}
)

func day(n int) time.Time { return time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC).AddDate(0, 0, n) }

var testRepo = model.TargetRepo{
	ID:             1,
	Slug:           "server",
	URL:            "https://github.com/example-org/example-server.git",
	DefaultBranch:  "main",
	LastIndexedSHA: sha1,
	LastFetchAt:    t0,
}

// mustOK fails the test unless rec is a 200 HTML page.
func mustOK(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body:\n%s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
}

func TestRepos_List(t *testing.T) {
	t.Parallel()
	repos := []model.TargetRepo{
		testRepo,
		{ID: 2, Slug: "web app", URL: "https://github.com/example-org/example-web.git", DefaultBranch: "trunk",
			LastIndexedSHA: "abc", LastFetchAt: t0.Add(90 * time.Minute).In(time.FixedZone("X", 3600)),
			LastFetchError: `git fetch: exit status 128: fatal: "repo" <not> found & gone`},
		{ID: 3, Slug: "fresh", URL: "https://github.com/example-org/example-fresh.git"},
	}
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, &fakeTimeline{repos: repos})
	rec := get(t, s.Handler(), "/repos")
	mustOK(t, rec)
	assertGolden(t, "repos_list", rec.Body.Bytes())
}

func TestRepos_Empty(t *testing.T) {
	t.Parallel()
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, &fakeTimeline{})
	rec := get(t, s.Handler(), "/repos")
	mustOK(t, rec)
	if !strings.Contains(rec.Body.String(), "No repositories indexed yet.") {
		t.Errorf("body lacks the empty state")
	}
	assertGolden(t, "repos_empty", rec.Body.Bytes())
}

func TestRepos_ReaderError(t *testing.T) {
	t.Parallel()
	readErr := errors.New("sqlite: no such table: repos")
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, &fakeTimeline{err: readErr})
	rec := get(t, s.Handler(), "/repos")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), readErr.Error()) {
		t.Errorf("body contains the reader error")
	}
}

func TestTimeline_All(t *testing.T) {
	t.Parallel()
	intervals := []model.DepInterval{
		{ID: 1, ManifestPath: "go.mod", Package: pkgGin, Version: "v1.9.0", Requirement: "v1.9.0", Direct: model.DirectYes,
			FromSHA: "a1", From: day(0), ToSHA: "b2", To: day(10)},
		{ID: 2, ManifestPath: "go.mod", Package: pkgGin, Version: "v1.10.0", Requirement: "v1.10.0", Direct: model.DirectYes,
			FromSHA: "b2", From: day(10)},
		{ID: 3, ManifestPath: "go.mod", Package: pkgToml, Version: "v1.3.2", Direct: model.DirectNo,
			FromSHA: "a1", From: day(0).In(time.FixedZone("X", -10*3600))},
		{ID: 4, ManifestPath: "deploy/Dockerfile", Package: pkgImage, Requirement: "3.19", Direct: model.DirectUnknown,
			FromSHA: "c3", From: day(5), ToSHA: "d4", To: day(20)},
	}
	tl := &fakeTimeline{repo: testRepo, intervals: intervals}
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, tl)
	rec := get(t, s.Handler(), "/repos/server")
	mustOK(t, rec)
	if tl.gotSlug != "server" || tl.gotFilter != (query.TimelineFilter{}) {
		t.Errorf("Timeline called with %q, %+v; want server and a zero filter", tl.gotSlug, tl.gotFilter)
	}
	body := rec.Body.String()
	for _, want := range []string{`<tr class="open">`, "(3.19)", "<td>now</td>", "<td>yes</td>", "<td>no</td>", "<td>?</td>",
		`href="?pkg=pkg%3Agolang%2Fgithub.com%2Fgin-gonic%2Fgin"`, "0123456789ab<"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	assertGolden(t, "timeline_all", rec.Body.Bytes())
}

func TestTimeline_NotIndexed(t *testing.T) {
	t.Parallel()
	repo := model.TargetRepo{ID: 3, Slug: "fresh", URL: "https://github.com/example-org/example-fresh.git"}
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, &fakeTimeline{repo: repo})
	rec := get(t, s.Handler(), "/repos/fresh")
	mustOK(t, rec)
	body := rec.Body.String()
	if !strings.Contains(body, "Not indexed yet.") || strings.Contains(body, "No intervals match.") {
		t.Errorf("body does not show only the not-indexed state:\n%s", body)
	}
}

func TestTimeline_NoMatch(t *testing.T) {
	t.Parallel()
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, &fakeTimeline{repo: testRepo})
	rec := get(t, s.Handler(), "/repos/server?q=nothing")
	mustOK(t, rec)
	body := rec.Body.String()
	if !strings.Contains(body, "No intervals match.") || strings.Contains(body, "Not indexed yet.") {
		t.Errorf("body does not show only the no-match state:\n%s", body)
	}
}

func TestTimeline_PackageHistory(t *testing.T) {
	t.Parallel()
	intervals := []model.DepInterval{
		{ID: 1, ManifestPath: "go.mod", Package: pkgGin, Version: "v1.9.0", Direct: model.DirectYes,
			FromSHA: "a1", From: day(0), ToSHA: "b2", To: day(10)},
		{ID: 2, ManifestPath: "go.mod", Package: pkgGin, Version: "v1.10.0", Direct: model.DirectYes,
			FromSHA: "b2", From: day(10)},
	}
	tl := &fakeTimeline{repo: testRepo, intervals: intervals}
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, tl)
	rec := get(t, s.Handler(), "/repos/server?pkg=pkg%3Agolang%2Fgithub.com%2Fgin-gonic%2Fgin&on=2026-01-05")
	mustOK(t, rec)
	if tl.gotFilter.Package != pkgGin.PURL {
		t.Errorf("filter Package = %q, want %q", tl.gotFilter.Package, pkgGin.PURL)
	}
	body := rec.Body.String()
	for _, want := range []string{"History of pkg:golang/github.com/gin-gonic/gin", `href="/repos/server?on=2026-01-05">Back to all packages`,
		`href="?pkg=pkg%3Agolang%2Fgithub.com%2Fgin-gonic%2Fgin&amp;on=2026-01-05"`, `name="pkg" value="pkg:golang/github.com/gin-gonic/gin"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	assertGolden(t, "timeline_package", rec.Body.Bytes())
}

func TestTimeline_FilterPassedThrough(t *testing.T) {
	t.Parallel()
	tl := &fakeTimeline{repo: testRepo}
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, tl)
	rec := get(t, s.Handler(), "/repos/server?q=gin&on=2026-03-01")
	mustOK(t, rec)
	want := query.TimelineFilter{Contains: "gin", On: time.Date(2026, 3, 1, 23, 59, 59, 999999999, time.UTC)}
	if tl.gotFilter != want {
		t.Errorf("Timeline filter = %+v, want %+v", tl.gotFilter, want)
	}
	if !tl.gotFilter.On.Equal(want.On) || tl.gotFilter.On.Location() != time.UTC {
		t.Errorf("filter On = %v, want %v in UTC", tl.gotFilter.On, want.On)
	}
	body := rec.Body.String()
	for _, prefilled := range []string{`name="q" value="gin"`, `name="on" value="2026-03-01"`} {
		if !strings.Contains(body, prefilled) {
			t.Errorf("form not prefilled: body lacks %q", prefilled)
		}
	}
}

func TestTimeline_BadDate(t *testing.T) {
	t.Parallel()
	for _, on := range []string{"2026-13-01", "01-03-2026", "yesterday", "2026-03-01T00:00:00Z"} {
		t.Run(on, func(t *testing.T) {
			t.Parallel()
			tl := &fakeTimeline{repo: testRepo}
			s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, tl)
			rec := get(t, s.Handler(), "/repos/server?on="+on)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("GET ?on=%s status = %d, want 400", on, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "YYYY-MM-DD") {
				t.Errorf("body lacks the date format message")
			}
			if tl.calls != 0 {
				t.Errorf("reader called %d times, want 0", tl.calls)
			}
		})
	}
}

func TestTimeline_UnknownRepo(t *testing.T) {
	t.Parallel()
	tl := &fakeTimeline{err: fmt.Errorf("read timeline of %q: %w", "missing", model.ErrNotFound)}
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, tl)
	rec := get(t, s.Handler(), "/repos/missing")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
}

func TestTimeline_ReaderError(t *testing.T) {
	t.Parallel()
	readErr := errors.New("sqlite: database disk image is malformed")
	var logs bytes.Buffer
	tl := &fakeTimeline{err: readErr}
	s := newLoggingTimelineServer(t, tl, &logs)
	rec := get(t, s.Handler(), "/repos/server")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), readErr.Error()) {
		t.Errorf("body contains the reader error:\n%s", rec.Body)
	}
	if !strings.Contains(logs.String(), readErr.Error()) {
		t.Errorf("log %q lacks the reader error", logs.String())
	}
}

func TestTimeline_Escapes(t *testing.T) {
	t.Parallel()
	intervals := []model.DepInterval{
		{ID: 1, ManifestPath: `sub "dir" & more/package.json`, Package: model.Package{PURL: "pkg:npm/evil"},
			Version: "<script>alert(1)</script>", Direct: model.DirectYes, FromSHA: "a1", From: day(0)},
	}
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, &fakeTimeline{repo: testRepo, intervals: intervals})
	rec := get(t, s.Handler(), "/repos/server")
	mustOK(t, rec)
	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Errorf("body contains a raw <script>:\n%s", body)
	}
	for _, want := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "sub &#34;dir&#34; &amp; more/package.json"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks escaped %q", want)
		}
	}
}

func TestRepos_HostCheck(t *testing.T) {
	t.Parallel()
	s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{}, &fakeTimeline{repo: testRepo})
	for _, path := range []string{"/repos", "/repos/server"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		req.Host = "attacker.example"
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMisdirectedRequest {
			t.Errorf("GET %s from attacker.example status = %d, want 421", path, rec.Code)
		}
	}
}
