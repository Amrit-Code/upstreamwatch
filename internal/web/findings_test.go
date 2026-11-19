package web_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/findings"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/web"
)

var _ web.FindingActions = (*findings.Service)(nil)

// date is 09:30 UTC on the given day.
func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 9, 30, 0, 0, time.UTC) }

// hostile is upstream text that must render escaped.
const hostile = "<script>alert(1)</script> </pre> javascript:alert(1)"

var findingRepos = []model.TargetRepo{
	{ID: 5, Slug: "example-client"},
	{ID: 2, Slug: "example-server"},
}

var pkgLib = model.Package{PURL: "pkg:golang/github.com/example-org/example-lib", Ecosystem: "golang",
	Name: "github.com/example-org/example-lib"}

// advisoryDetail is an acked advisory finding linked to an open interval,
// with one closed exposure whose start differs from its pin and one open.
func advisoryDetail() model.FindingDetail {
	closed := model.DepInterval{ID: 30, ManifestPath: "go.mod", Package: pkgLib, Version: "v1.1.0",
		FromSHA: "a1", From: date(2025, 11, 3), ToSHA: "b2", To: date(2026, 1, 20)}
	open := model.DepInterval{ID: 31, ManifestPath: "go.mod", Package: pkgLib, Version: "v1.2.0",
		FromSHA: "b2", From: date(2026, 1, 20)}
	return model.FindingDetail{
		Finding: model.Finding{
			ID: 12, Kind: model.KindAdvisory, RepoID: 2, PackageID: 4, DepVersionID: 31,
			AdvisoryID: "GHSA-xxxx-yyyy-zzzz", Fingerprint: "advisory|2|GHSA-xxxx-yyyy-zzzz",
			Score: 8.44, RankReason: "high severity, still exposed, known exploited",
			FirstSeenAt: t0.Add(-48 * time.Hour).In(time.FixedZone("X", -5*3600)),
			State: model.FindingState{State: model.StateAcked, Note: "upgrade planned\nnext sprint <b>",
				ChangedAt: t0.Add(-time.Hour), ChangedBy: "web"},
		},
		RepoSlug: "example-server",
		Package:  pkgLib,
		Interval: open,
		Advisory: model.Advisory{
			ID:         "GHSA-xxxx-yyyy-zzzz",
			Aliases:    []string{"CVE-2026-12345", "GO-2026-0001"},
			Summary:    "Path traversal in example-lib " + hostile,
			Details:    "Line one.\n\n**not markdown** <b>bold</b>\n" + hostile + "\n[link](javascript:alert(1))",
			Severity:   model.SeverityHigh,
			CVSSScore:  8.1,
			CVSSVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N",
			Published:  date(2026, 1, 10),
			Modified:   date(2026, 2, 1),
			KEV:        true, KEVAddedAt: date(2026, 2, 15),
			HasEPSS: true, EPSS: 0.97412, EPSSPercentile: 0.99871,
		},
		Exposures: []model.Exposure{
			{Match: model.AdvisoryMatch{ID: 1, RepoID: 2, DepVersionID: 30, AdvisoryID: "GHSA-xxxx-yyyy-zzzz",
				ExposedFrom: date(2026, 1, 10), ExposedTo: date(2026, 1, 20), FixedVersion: "v1.2.1",
				Confidence: model.ConfidenceHigh, ConfidenceReason: "exact version inside a SEMVER range"},
				Interval: closed},
			{Match: model.AdvisoryMatch{ID: 2, RepoID: 2, DepVersionID: 31, AdvisoryID: "GHSA-xxxx-yyyy-zzzz",
				ExposedFrom: date(2026, 1, 20), FixedVersion: "v1.2.1",
				Confidence: model.ConfidenceMedium, ConfidenceReason: "ECOSYSTEM range " + hostile},
				Interval: open},
		},
	}
}

func jobFailureDetail() model.FindingDetail {
	return model.FindingDetail{Finding: model.Finding{
		ID: 7, Kind: model.KindJobFailure, Fingerprint: "job_failure|ingest", Score: 50,
		RankReason:  "job ingest failed: fetch example-org/example-server: 502 bad gateway",
		FirstSeenAt: t0, State: model.FindingState{State: model.StateOpen},
	}}
}

// listDetails are, in score order: a job_failure, an acked advisory, an
// advisory whose snooze has ended (open at t0) and a dismissed advisory.
func listDetails() []model.FindingDetail {
	expired := model.FindingDetail{
		Finding: model.Finding{ID: 10, Kind: model.KindAdvisory, RepoID: 2, PackageID: 4, AdvisoryID: "GO-2026-0002",
			Score: 5, FirstSeenAt: t0.Add(-72 * time.Hour),
			State: model.FindingState{State: model.StateSnoozed, SnoozeUntil: t0.Add(-time.Hour)}},
		RepoSlug: "example-server", Package: pkgLib,
		Advisory: model.Advisory{ID: "GO-2026-0002"},
	}
	dismissed := model.FindingDetail{
		Finding: model.Finding{ID: 9, Kind: model.KindAdvisory, RepoID: 5, PackageID: 6, DepVersionID: 40,
			AdvisoryID: "GHSA-dddd-eeee-ffff", Score: 3.2, FirstSeenAt: t0.Add(-96 * time.Hour),
			State: model.FindingState{State: model.StateDismissed, Note: "not reachable"}},
		RepoSlug: "example-client",
		Package:  model.Package{PURL: "pkg:npm/example-widget"},
		Interval: model.DepInterval{ID: 40, ManifestPath: "package.json", Requirement: "^2.0.0"},
		Advisory: model.Advisory{ID: "GHSA-dddd-eeee-ffff", Severity: model.SeverityLow},
		Exposures: []model.Exposure{{Match: model.AdvisoryMatch{DepVersionID: 40, Confidence: model.ConfidenceLow,
			ExposedFrom: date(2026, 2, 1)}}},
	}
	return []model.FindingDetail{jobFailureDetail(), advisoryDetail(), expired, dismissed}
}

func TestFindingsList_Golden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		golden  string
		details []model.FindingDetail
	}{
		{name: "empty", path: "/", golden: "findings_empty", details: nil},
		{name: "dismissed hidden", path: "/", golden: "findings_list", details: listDetails()},
		{name: "suppressed shown", path: "/?suppressed=1", golden: "findings_list_suppressed", details: listDetails()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTimelineServer(t, &fakeHealth{}, &fakeFindings{details: tt.details}, &fakeTimeline{repos: findingRepos})
			rec := get(t, s.Handler(), tt.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", tt.path, rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
			}
			assertGolden(t, tt.golden, rec.Body.Bytes())
		})
	}
}

func TestFindingsList_EmptyMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{path: "/", want: "No open findings."},
		{path: "/?suppressed=1", want: "No findings."},
		{path: "/?suppressed=yes", want: "No open findings."},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			s := newTestServer(t, &fakeHealth{}, &fakeFindings{})
			rec := get(t, s.Handler(), tt.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", tt.path, rec.Code)
			}
			if want := `<p class="empty">` + tt.want + `</p>`; !strings.Contains(rec.Body.String(), want) {
				t.Errorf("GET %s body lacks %q:\n%s", tt.path, want, rec.Body.String())
			}
		})
	}
}

func TestFindings_Filter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want model.FindingFilter
	}{
		{path: "/", want: model.FindingFilter{}},
		{path: "/?repo=&kind=", want: model.FindingFilter{}},
		{path: "/?repo=example-server", want: model.FindingFilter{RepoID: 2}},
		{path: "/?kind=job_failure", want: model.FindingFilter{Kind: model.KindJobFailure}},
		{path: "/?repo=example-client&kind=advisory&suppressed=1",
			want: model.FindingFilter{RepoID: 5, Kind: model.KindAdvisory, Suppressed: true}},
		{path: "/?kind=issue&suppressed=0", want: model.FindingFilter{Kind: model.KindIssue}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			f := &fakeFindings{}
			s := newTimelineServer(t, &fakeHealth{}, f, &fakeTimeline{repos: findingRepos})
			rec := get(t, s.Handler(), tt.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", tt.path, rec.Code)
			}
			if got := f.filters(); !reflect.DeepEqual(got, []model.FindingFilter{tt.want}) {
				t.Errorf("GET %s filters = %+v, want [%+v]", tt.path, got, tt.want)
			}
		})
	}
}

func TestFindings_BadFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path string
		wantMsg    string
	}{
		{name: "unknown repo", path: "/?repo=nope", wantMsg: "No repository with that name."},
		{name: "bad kind", path: "/?kind=verdict", wantMsg: "The kind must be advisory, issue or job_failure."},
		{name: "kind with wrong case", path: "/?kind=Advisory", wantMsg: "The kind must be advisory, issue or job_failure."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeFindings{details: listDetails()}
			s := newTimelineServer(t, &fakeHealth{}, f, &fakeTimeline{repos: findingRepos})
			rec := get(t, s.Handler(), tt.path)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("GET %s status = %d, want 400", tt.path, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tt.wantMsg) {
				t.Errorf("GET %s body lacks %q:\n%s", tt.path, tt.wantMsg, rec.Body.String())
			}
			if got := f.filters(); len(got) != 0 {
				t.Errorf("GET %s called Findings with %+v, want no call", tt.path, got)
			}
		})
	}
}

func TestFindings_ReadErrors(t *testing.T) {
	t.Parallel()
	readErr := errors.New("sqlite: no such table: findings")
	tests := []struct {
		name string
		f    *fakeFindings
		tl   *fakeTimeline
	}{
		{name: "findings fail", f: &fakeFindings{err: readErr}, tl: &fakeTimeline{}},
		{name: "repos fail", f: &fakeFindings{}, tl: &fakeTimeline{err: readErr}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			s := newFindingsServer(t, tt.f, &fakeActions{}, tt.tl, slogTo(&logs))
			rec := get(t, s.Handler(), "/")
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}
			if strings.Contains(rec.Body.String(), readErr.Error()) {
				t.Errorf("body contains the read error:\n%s", rec.Body.String())
			}
			if !strings.Contains(logs.String(), readErr.Error()) {
				t.Errorf("log %q lacks the read error", logs.String())
			}
		})
	}
}

func TestFindingPage_Golden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		id     string
		golden string
	}{
		{name: "advisory", id: "12", golden: "finding_advisory"},
		{name: "job failure", id: "7", golden: "finding_job_failure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeFindings{details: listDetails()}
			s := newTestServer(t, &fakeHealth{}, f)
			rec := get(t, s.Handler(), "/f/"+tt.id)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /f/%s status = %d, want 200", tt.id, rec.Code)
			}
			body := rec.Body.Bytes()
			if bytes.Contains(body, []byte("<script>")) || bytes.Contains(body, []byte("</pre> ")) ||
				bytes.Contains(body, []byte("<b>")) {
				t.Errorf("body contains unescaped markup:\n%s", body)
			}
			assertGolden(t, tt.golden, body)
		})
	}
}

func TestFindingPage_ReopenOnlyWhenNotOpen(t *testing.T) {
	t.Parallel()
	running := jobFailureDetail()
	running.Finding.ID = 8
	running.Finding.State = model.FindingState{State: model.StateSnoozed, SnoozeUntil: t0.Add(24 * time.Hour)}
	ended := jobFailureDetail()
	ended.Finding.ID = 6
	ended.Finding.State = model.FindingState{State: model.StateSnoozed, SnoozeUntil: t0}
	f := &fakeFindings{details: []model.FindingDetail{jobFailureDetail(), running, ended, advisoryDetail()}}
	s := newTestServer(t, &fakeHealth{}, f)
	tests := []struct {
		id         string
		wantReopen bool
		wantState  string
	}{
		{id: "7", wantReopen: false, wantState: "<dd>open</dd>"},
		{id: "8", wantReopen: true, wantState: "<dd>snoozed until 2026-03-02 12:00 (UTC)</dd>"},
		{id: "6", wantReopen: false, wantState: "<dd>open</dd>"},
		{id: "12", wantReopen: true, wantState: "<dd>acked</dd>"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()
			body := get(t, s.Handler(), "/f/"+tt.id).Body.String()
			if got := strings.Contains(body, `action="/f/`+tt.id+`/reopen"`); got != tt.wantReopen {
				t.Errorf("GET /f/%s has reopen form = %v, want %v", tt.id, got, tt.wantReopen)
			}
			if !strings.Contains(body, tt.wantState) {
				t.Errorf("GET /f/%s body lacks %q", tt.id, tt.wantState)
			}
		})
	}
}

func TestFindingPage_NotFound(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/f/999", "/f/abc", "/f/1.5", "/f/99999999999999999999"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			s := newTestServer(t, &fakeHealth{}, &fakeFindings{details: listDetails()})
			rec := get(t, s.Handler(), path)
			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want 404", path, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "No finding with that ID.") {
				t.Errorf("GET %s body lacks the not-found message:\n%s", path, rec.Body.String())
			}
		})
	}
}

func TestFindingPage_ReadError(t *testing.T) {
	t.Parallel()
	readErr := errors.New("sqlite: database is locked")
	var logs bytes.Buffer
	s := newFindingsServer(t, &fakeFindings{detailErr: readErr}, &fakeActions{}, &fakeTimeline{}, slogTo(&logs))
	rec := get(t, s.Handler(), "/f/12")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), readErr.Error()) {
		t.Errorf("body contains the read error:\n%s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), readErr.Error()) {
		t.Errorf("log %q lacks the read error", logs.String())
	}
}

// post sends a same-origin form POST to path through h.
func post(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Host = testHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// postActions builds a Server around a fakeActions returning err.
func postActions(t *testing.T, err error) (*web.Server, *fakeActions) {
	t.Helper()
	a := &fakeActions{err: err}
	return newFindingsServer(t, &fakeFindings{}, a, &fakeTimeline{}, discardLogger()), a
}

// assertRedirect checks rec is a 303 to location.
func assertRedirect(t *testing.T, rec *httptest.ResponseRecorder, location string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body:\n%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != location {
		t.Errorf("Location = %q, want %q", got, location)
	}
}

func assertCalls(t *testing.T, a *fakeActions, want []actionCall) {
	t.Helper()
	if got := a.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %+v, want %+v", got, want)
	}
}

func TestAck_PostRedirect(t *testing.T) {
	t.Parallel()
	s, a := postActions(t, nil)
	rec := post(t, s.Handler(), "/f/12/ack", url.Values{"note": {"will upgrade"}})
	assertRedirect(t, rec, "/f/12")
	assertCalls(t, a, []actionCall{{Method: "Ack", ID: 12, Note: "will upgrade", By: "web"}})
}

func TestAck_PostWithoutNote(t *testing.T) {
	t.Parallel()
	s, a := postActions(t, nil)
	rec := post(t, s.Handler(), "/f/3/ack", url.Values{})
	assertRedirect(t, rec, "/f/3")
	assertCalls(t, a, []actionCall{{Method: "Ack", ID: 3, By: "web"}})
}

func TestSnooze_Post(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  time.Duration
	}{
		{value: "1d", want: 24 * time.Hour},
		{value: "7d", want: 168 * time.Hour},
		{value: "30d", want: 720 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			s, a := postActions(t, nil)
			rec := post(t, s.Handler(), "/f/12/snooze", url.Values{"for": {tt.value}, "note": {"after the release"}})
			assertRedirect(t, rec, "/f/12")
			assertCalls(t, a, []actionCall{{Method: "Snooze", ID: 12, For: tt.want, Note: "after the release", By: "web"}})
		})
	}
}

func TestSnooze_BadFor(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "2d", "7", "168h", "7D"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			s, a := postActions(t, nil)
			rec := post(t, s.Handler(), "/f/12/snooze", url.Values{"for": {value}})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("for=%q status = %d, want 400", value, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "The snooze length must be 1d, 7d or 30d.") {
				t.Errorf("for=%q body lacks the message:\n%s", value, rec.Body.String())
			}
			assertCalls(t, a, nil)
		})
	}
}

func TestDismiss_Post(t *testing.T) {
	t.Parallel()
	s, a := postActions(t, nil)
	rec := post(t, s.Handler(), "/f/9/dismiss", url.Values{"note": {"line one\r\nline two\rline three\n"}})
	assertRedirect(t, rec, "/f/9")
	assertCalls(t, a, []actionCall{{Method: "Dismiss", ID: 9, Note: "line one\nline two\nline three\n", By: "web"}})
}

func TestReopen_Post(t *testing.T) {
	t.Parallel()
	s, a := postActions(t, nil)
	rec := post(t, s.Handler(), "/f/9/reopen", url.Values{"note": {"ignored"}})
	assertRedirect(t, rec, "/f/9")
	assertCalls(t, a, []actionCall{{Method: "Reopen", ID: 9, By: "web"}})
}

func TestPost_InvalidTransition(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"ack", "snooze", "dismiss", "reopen"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			s, a := postActions(t, fmt.Errorf("%s finding 12: finding 12 is resolved: %w", action, model.ErrInvalidTransition))
			rec := post(t, s.Handler(), "/f/12/"+action, url.Values{"for": {"1d"}, "note": {"x"}})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST %s status = %d, want 400", action, rec.Code)
			}
			if body := rec.Body.String(); !strings.Contains(body, "The finding was not changed") ||
				!strings.Contains(body, "is resolved") {
				t.Errorf("POST %s body lacks the reason:\n%s", action, body)
			}
			if got := len(a.recorded()); got != 1 {
				t.Errorf("POST %s calls = %d, want 1", action, got)
			}
		})
	}
}

func TestPost_NotFound(t *testing.T) {
	t.Parallel()
	t.Run("unknown id", func(t *testing.T) {
		t.Parallel()
		for _, action := range []string{"ack", "snooze", "dismiss", "reopen"} {
			s, _ := postActions(t, fmt.Errorf("get finding 999: %w", model.ErrNotFound))
			rec := post(t, s.Handler(), "/f/999/"+action, url.Values{"for": {"30d"}})
			if rec.Code != http.StatusNotFound {
				t.Errorf("POST %s status = %d, want 404", action, rec.Code)
			}
		}
	})
	t.Run("non-numeric id", func(t *testing.T) {
		t.Parallel()
		s, a := postActions(t, nil)
		rec := post(t, s.Handler(), "/f/abc/ack", url.Values{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
		assertCalls(t, a, nil)
	})
}

func TestPost_ServiceError(t *testing.T) {
	t.Parallel()
	saveErr := errors.New("sqlite: disk I/O error")
	var logs bytes.Buffer
	a := &fakeActions{err: fmt.Errorf("ack finding 12: %w", saveErr)}
	s := newFindingsServer(t, &fakeFindings{}, a, &fakeTimeline{}, slogTo(&logs))
	rec := post(t, s.Handler(), "/f/12/ack", url.Values{})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), saveErr.Error()) {
		t.Errorf("body contains the service error:\n%s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), saveErr.Error()) {
		t.Errorf("log %q lacks the service error", logs.String())
	}
}

func TestPost_BodyTooLarge(t *testing.T) {
	t.Parallel()
	s, a := postActions(t, nil)
	rec := post(t, s.Handler(), "/f/12/ack", url.Values{"note": {strings.Repeat("a", 16<<10)}})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	assertCalls(t, a, nil)
}

func TestPost_CrossOrigin(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"ack", "snooze", "dismiss", "reopen"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			s, a := postActions(t, nil)
			form := url.Values{"for": {"7d"}, "note": {"x"}}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/f/1/"+action, strings.NewReader(form.Encode()))
			req.Host = testHost
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "https://evil.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("cross-site POST %s status = %d, want 403", action, rec.Code)
			}
			assertCalls(t, a, nil)
		})
	}
}

func TestGet_NeverChangesState(t *testing.T) {
	t.Parallel()
	a := &fakeActions{}
	s := newFindingsServer(t, &fakeFindings{details: listDetails()}, a, &fakeTimeline{repos: findingRepos}, discardLogger())
	tests := []struct {
		path string
		want int
	}{
		{path: "/", want: http.StatusOK},
		{path: "/?suppressed=1", want: http.StatusOK},
		{path: "/f/12", want: http.StatusOK},
		{path: "/f/12/ack", want: http.StatusMethodNotAllowed},
		{path: "/f/12/snooze?for=7d", want: http.StatusMethodNotAllowed},
		{path: "/f/12/dismiss", want: http.StatusMethodNotAllowed},
		{path: "/f/12/reopen", want: http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		if got := get(t, s.Handler(), tt.path).Code; got != tt.want {
			t.Errorf("GET %s status = %d, want %d", tt.path, got, tt.want)
		}
	}
	assertCalls(t, a, nil)
}
