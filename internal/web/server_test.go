package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/web"
)

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

const wantCSP = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'"

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type jsonRun struct {
	Job        string  `json:"job"`
	Status     string  `json:"status"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Error      string  `json:"error"`
}

type jsonHealth struct {
	OK       bool      `json:"ok"`
	LastRuns []jsonRun `json:"last_runs"`
}

func TestHealth_Shape(t *testing.T) {
	t.Parallel()

	t.Run("no runs", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t, &fakeHealth{}, &fakeFindings{})
		rec := get(t, s.Handler(), "/health")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		got := strings.TrimSuffix(rec.Body.String(), "\n")
		if want := `{"ok":true,"last_runs":[]}`; got != want {
			t.Errorf("body = %s, want %s", got, want)
		}
	})

	t.Run("two runs one running", func(t *testing.T) {
		t.Parallel()
		runs := []model.Run{
			{ID: 1, Job: "ingest", StartedAt: t0, FinishedAt: t0.Add(5 * time.Second), Status: model.RunSucceeded},
			// Sub-second and non-UTC start time, to check formatting.
			{ID: 2, Job: "vulns", StartedAt: t0.Add(1500 * time.Millisecond).In(time.FixedZone("X", 3600)),
				Status: model.RunRunning},
		}
		s := newTestServer(t, &fakeHealth{runs: runs}, &fakeFindings{})
		rec := get(t, s.Handler(), "/health")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		var got jsonHealth
		dec := json.NewDecoder(strings.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !got.OK {
			t.Errorf("ok = false, want true")
		}
		if len(got.LastRuns) != 2 {
			t.Fatalf("len(last_runs) = %d, want 2", len(got.LastRuns))
		}
		first, second := got.LastRuns[0], got.LastRuns[1]
		if first.Job != "ingest" || first.Status != "succeeded" || first.StartedAt != "2026-03-01T12:00:00Z" || first.Error != "" {
			t.Errorf("last_runs[0] = %+v, want ingest succeeded 2026-03-01T12:00:00Z", first)
		}
		if first.FinishedAt == nil || *first.FinishedAt != "2026-03-01T12:00:05Z" {
			t.Errorf("last_runs[0].finished_at = %v, want 2026-03-01T12:00:05Z", first.FinishedAt)
		}
		if second.Job != "vulns" || second.Status != "running" || second.StartedAt != "2026-03-01T12:00:01Z" {
			t.Errorf("last_runs[1] = %+v, want vulns running 2026-03-01T12:00:01Z", second)
		}
		if second.FinishedAt != nil {
			t.Errorf("last_runs[1].finished_at = %q, want null", *second.FinishedAt)
		}
		if !strings.Contains(body, `"finished_at":null`) {
			t.Errorf("body %s lacks \"finished_at\":null", body)
		}
	})
}

func TestHealth_StoreError(t *testing.T) {
	t.Parallel()
	storeErr := errors.New("sqlite: database disk image is malformed")
	var logs bytes.Buffer
	s, err := web.NewServer(&fakeHealth{err: storeErr}, &fakeFindings{}, web.Options{},
		slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	rec := get(t, s.Handler(), "/health")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	body := rec.Body.String()
	if got, want := strings.TrimSuffix(body, "\n"), `{"ok":false,"last_runs":[]}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	if strings.Contains(body, storeErr.Error()) {
		t.Errorf("body %q contains the store error", body)
	}
	if !strings.Contains(logs.String(), storeErr.Error()) {
		t.Errorf("log %q lacks the store error", logs.String())
	}
}

func TestFindingsPage_Golden(t *testing.T) {
	t.Parallel()
	threeFindings := []model.Finding{
		{ID: 7, Kind: model.KindJobFailure, Score: 9.96, RankReason: "job ingest failed twice in a row",
			FirstSeenAt: t0, State: model.FindingState{State: model.StateOpen}},
		{ID: 3, Kind: model.KindAdvisory, Score: 7.25,
			RankReason:  "GHSA-xxxx-yyyy-zzzz affects github.com/example-org/example-lib <script>alert(1)</script> </pre> javascript:alert(1)",
			FirstSeenAt: t0.Add(-26*time.Hour + 30*time.Second).In(time.FixedZone("X", -5*3600)),
			State:       model.FindingState{State: model.StateAcked}},
		{ID: 1, Kind: model.KindIssue, Score: 3, RankReason: "upstream issue \"leak & crash\" matches a used symbol",
			FirstSeenAt: t0.Add(-72 * time.Hour), State: model.FindingState{State: model.StateOpen}},
	}
	tests := []struct {
		name     string
		golden   string
		findings []model.Finding
	}{
		{name: "empty", golden: "findings_empty", findings: nil},
		{name: "three findings", golden: "findings_list", findings: threeFindings},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestServer(t, &fakeHealth{}, &fakeFindings{findings: tt.findings})
			rec := get(t, s.Handler(), "/")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
			}
			body := rec.Body.Bytes()
			if bytes.Contains(body, []byte("<script>")) || bytes.Contains(body, []byte("</pre>")) {
				t.Errorf("body contains unescaped markup:\n%s", body)
			}
			assertGolden(t, tt.golden, body)
		})
	}
}

func TestFindingsPage_StoreError(t *testing.T) {
	t.Parallel()
	storeErr := errors.New("sqlite: no such table: findings")
	s := newTestServer(t, &fakeHealth{}, &fakeFindings{err: storeErr})
	rec := get(t, s.Handler(), "/")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if got, want := strings.TrimSpace(body), "internal server error"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if strings.Contains(body, storeErr.Error()) {
		t.Errorf("body %q contains the store error", body)
	}
}

func TestStylesheet(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, &fakeHealth{}, &fakeFindings{})
	rec := get(t, s.Handler(), "/static/style.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/css; charset=utf-8", got)
	}
	if !strings.Contains(rec.Body.String(), ".empty") {
		t.Errorf("stylesheet lacks the empty-state rule")
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, &fakeHealth{}, &fakeFindings{})
	for _, path := range []string{"/", "/health", "/static/style.css", "/nope"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			rec := get(t, s.Handler(), path)
			if got := rec.Header().Get("Content-Security-Policy"); got != wantCSP {
				t.Errorf("GET %s Content-Security-Policy = %q, want %q", path, got, wantCSP)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("GET %s X-Content-Type-Options = %q, want nosniff", path, got)
			}
		})
	}
}

func TestRoutes_NotFoundAndMethod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		method, path string
		fetchSite    string
		want         int
	}{
		{name: "unknown path", method: http.MethodGet, path: "/nope", want: http.StatusNotFound},
		{name: "unknown static file", method: http.MethodGet, path: "/static/other.css", want: http.StatusNotFound},
		{name: "post health same origin", method: http.MethodPost, path: "/health", fetchSite: "same-origin", want: http.StatusMethodNotAllowed},
		{name: "post root cross site", method: http.MethodPost, path: "/", fetchSite: "cross-site", want: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestServer(t, &fakeHealth{}, &fakeFindings{})
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			if tt.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tt.fetchSite)
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("%s %s status = %d, want %d", tt.method, tt.path, rec.Code, tt.want)
			}
		})
	}
}

func TestNewServer_BadBaseURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{name: "empty", baseURL: "", wantErr: false},
		{name: "https origin", baseURL: "https://upw.example.net", wantErr: false},
		{name: "http with port and path", baseURL: "http://upw.example.net:8443/upw/", wantErr: false},
		{name: "relative", baseURL: "/upw", wantErr: true},
		{name: "no scheme", baseURL: "upw.example.net", wantErr: true},
		{name: "ftp scheme", baseURL: "ftp://upw.example.net", wantErr: true},
		{name: "unparsable", baseURL: "https://upw.example.net:bad port", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, err := web.NewServer(&fakeHealth{}, &fakeFindings{}, web.Options{BaseURL: tt.baseURL}, discardLogger())
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewServer(BaseURL %q) error = %v, wantErr %v", tt.baseURL, err, tt.wantErr)
			}
			if !tt.wantErr && s == nil {
				t.Errorf("NewServer(BaseURL %q) = nil server", tt.baseURL)
			}
		})
	}
}

func TestNewServer_TrustsBaseURLOrigin(t *testing.T) {
	t.Parallel()
	s, err := web.NewServer(&fakeHealth{}, &fakeFindings{},
		web.Options{BaseURL: "https://upw.example.net/"}, discardLogger())
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	// A trusted origin passes the cross-origin check, so the mux answers 405
	// for the GET-only route instead of 403.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://upw.example.net")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / from trusted origin status = %d, want 405", rec.Code)
	}
}

func TestServe_ShutsDownOnCancel(t *testing.T) {
	t.Parallel()
	capture := newAddrCapture()
	runs := []model.Run{{ID: 1, Job: "ingest", StartedAt: t0, FinishedAt: t0.Add(5 * time.Second), Status: model.RunSucceeded}}
	s, err := web.NewServer(&fakeHealth{runs: runs}, &fakeFindings{}, web.Options{Listen: "127.0.0.1:0"}, slog.New(capture))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()

	var addr string
	select {
	case addr = <-capture.addrs:
	case err := <-done:
		t.Fatalf("Serve() returned before listening: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("no \"http server listening\" log within 2s")
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/health", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	var got jsonHealth
	decErr := json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || decErr != nil || !got.OK || len(got.LastRuns) != 1 {
		t.Errorf("GET /health = %d %+v (decode error %v), want 200 ok with one run", resp.StatusCode, got, decErr)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve() error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not return within 2s of cancel")
	}
}

func TestServe_ListenError(t *testing.T) {
	t.Parallel()
	s, err := web.NewServer(&fakeHealth{}, &fakeFindings{}, web.Options{Listen: "127.0.0.1:not-a-port"}, discardLogger())
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if err := s.Serve(t.Context()); err == nil {
		t.Error("Serve() error = nil, want a listen error")
	}
}
