package kev_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/kev"
)

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var (
	_ vuln.ExploitSignals = (*kev.Feed)(nil)
	_ checker             = (*kev.Feed)(nil)
)

// newFeed returns a kev.Feed whose httpx client reaches loopback test servers
// and retries quickly.
func newFeed(url string) *kev.Feed {
	logger := slog.New(slog.DiscardHandler)
	hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}, MaxWait: time.Millisecond}, logger)
	return kev.New(hc, kev.Options{URL: url}, logger)
}

// serve returns a server that answers every request with body.
func serve(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization header sent, want none")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestKEVRefresh(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("testdata/kev.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := serve(t, body)

	got, err := newFeed(srv.URL + "/feeds/kev.json").Refresh(t.Context())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	want := []model.Signal{
		{Kind: model.SignalKEV, CVE: "CVE-2021-44228", KEVAddedAt: time.Date(2021, 12, 10, 0, 0, 0, 0, time.UTC)},
		{Kind: model.SignalKEV, CVE: "CVE-2023-44487"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Refresh() = %+v, want %+v", got, want)
	}
}

func TestKEVRefresh_Entries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want []model.Signal
	}{
		{
			name: "malformed date kept with zero time",
			body: `{"vulnerabilities":[{"cveID":"CVE-2024-12345","dateAdded":"10/12/2024"}]}`,
			want: []model.Signal{{Kind: model.SignalKEV, CVE: "CVE-2024-12345"}},
		},
		{
			name: "short sequence number skipped",
			body: `{"vulnerabilities":[{"cveID":"CVE-2024-123","dateAdded":"2024-01-02"}]}`,
			want: []model.Signal{},
		},
		{
			name: "lowercase id skipped",
			body: `{"vulnerabilities":[{"cveID":"cve-2024-1234","dateAdded":"2024-01-02"}]}`,
			want: []model.Signal{},
		},
		{
			name: "five-digit sequence number kept",
			body: `{"vulnerabilities":[{"cveID":"CVE-2024-123456","dateAdded":"2024-02-29"}]}`,
			want: []model.Signal{{Kind: model.SignalKEV, CVE: "CVE-2024-123456", KEVAddedAt: time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := serve(t, []byte(tt.body))
			got, err := newFeed(srv.URL).Refresh(t.Context())
			if err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Refresh() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestKEVRefresh_MalformedJSON(t *testing.T) {
	t.Parallel()
	srv := serve(t, []byte(`{"vulnerabilities":[`))

	_, err := newFeed(srv.URL + "/kev.json").Refresh(t.Context())
	if err == nil {
		t.Fatal("Refresh() = nil error, want one")
	}
	if got := err.Error(); !strings.HasPrefix(got, "kev: decode "+srv.URL+"/kev.json: ") {
		t.Errorf("Refresh() error = %q, want it to name the feed and URL", got)
	}
}

func TestKEVRefresh_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newFeed(srv.URL + "/kev.json").Refresh(t.Context())
	if err == nil {
		t.Fatal("Refresh() = nil error, want one")
	}
	got := err.Error()
	if !strings.HasPrefix(got, "kev: fetch "+srv.URL+"/kev.json: ") || !strings.Contains(got, "404") {
		t.Errorf("Refresh() error = %q, want it to name the feed, URL and status 404", got)
	}
}

func TestKEVRefresh_Cancelled(t *testing.T) {
	t.Parallel()
	srv := serve(t, []byte(`{"vulnerabilities":[]}`))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := newFeed(srv.URL).Refresh(ctx); err == nil {
		t.Fatal("Refresh() with a cancelled context = nil error, want one")
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "ok", status: http.StatusOK},
		{name: "server error", status: http.StatusInternalServerError, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotMethod, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			err := newFeed(srv.URL + "/kev.json").Check(t.Context())
			host := strings.TrimPrefix(srv.URL, "http://")
			switch {
			case tt.wantErr && err == nil:
				t.Fatal("Check() = nil, want an error")
			case tt.wantErr && !strings.Contains(err.Error(), host):
				t.Errorf("Check() = %q, want it to name %s", err, host)
			case !tt.wantErr && err != nil:
				t.Fatalf("Check() = %v, want nil", err)
			}
			if gotMethod != http.MethodHead || gotPath != "/kev.json" {
				t.Errorf("request = %s %s, want HEAD /kev.json", gotMethod, gotPath)
			}
		})
	}
}

func TestCheck_ServerDown(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	err := newFeed(url).Check(t.Context())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}
	if host := strings.TrimPrefix(url, "http://"); !strings.Contains(err.Error(), host) {
		t.Errorf("Check() = %q, want it to name %s", err, host)
	}
}
