package epss_test

import (
	"context"
	"fmt"
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
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/epss"
)

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var (
	_ vuln.ExploitSignals = (*epss.Feed)(nil)
	_ checker             = (*epss.Feed)(nil)
)

// fixtureSignals are the rows of testdata/epss_scores.csv.
var fixtureSignals = []model.Signal{
	{Kind: model.SignalEPSS, CVE: "CVE-1999-0001", EPSS: 0.01141, EPSSPercentile: 0.77923},
	{Kind: model.SignalEPSS, CVE: "CVE-2021-44228", EPSS: 0.94358, EPSSPercentile: 0.99957},
	{Kind: model.SignalEPSS, CVE: "CVE-2023-44487", EPSS: 0.82461, EPSSPercentile: 0.99187},
	{Kind: model.SignalEPSS, CVE: "CVE-2024-12345", EPSS: 0.00043, EPSSPercentile: 0.11520},
	{Kind: model.SignalEPSS, CVE: "CVE-2026-0001", EPSS: 0, EPSSPercentile: 1},
}

// newFeed returns an epss.Feed whose httpx client reaches loopback test
// servers and retries quickly.
func newFeed(url string) *epss.Feed {
	logger := slog.New(slog.DiscardHandler)
	hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}, MaxWait: time.Millisecond}, logger)
	return epss.New(hc, epss.Options{URL: url}, logger)
}

// serve returns a server that answers every request with body.
func serve(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization header sent, want none")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEPSSRefresh_Gzip(t *testing.T) {
	t.Parallel()
	srv := serve(t, readFixture(t, "epss_scores.csv.gz"))

	got, err := newFeed(srv.URL + "/epss_scores-current.csv.gz").Refresh(t.Context())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !reflect.DeepEqual(got, fixtureSignals) {
		t.Errorf("Refresh() = %+v, want %+v", got, fixtureSignals)
	}
}

func TestEPSSRefresh_Plain(t *testing.T) {
	t.Parallel()
	srv := serve(t, readFixture(t, "epss_scores.csv"))

	got, err := newFeed(srv.URL + "/epss_scores-current.csv").Refresh(t.Context())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !reflect.DeepEqual(got, fixtureSignals) {
		t.Errorf("Refresh() = %+v, want %+v", got, fixtureSignals)
	}
}

func TestEPSSRefresh_Redirect(t *testing.T) {
	t.Parallel()
	b := serve(t, readFixture(t, "epss_scores.csv.gz"))
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.URL+"/epss_scores-current.csv.gz", http.StatusMovedPermanently)
	}))
	defer a.Close()

	got, err := newFeed(a.URL + "/epss_scores-current.csv.gz").Refresh(t.Context())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !reflect.DeepEqual(got, fixtureSignals) {
		t.Errorf("Refresh() = %+v, want %+v", got, fixtureSignals)
	}
}

// csvWith returns a feed with the comment line, the header, good rows and
// then the given bad rows.
func csvWith(good int, bad ...string) string {
	var sb strings.Builder
	sb.WriteString("#model_version:v2025.03.14,score_date:2026-11-19T12:55:00Z\ncve,epss,percentile\n")
	for i := range good {
		fmt.Fprintf(&sb, "CVE-2026-%04d,0.5,0.5\n", i+1)
	}
	for _, row := range bad {
		sb.WriteString(row + "\n")
	}
	return sb.String()
}

func TestEPSSRefresh_BadRows(t *testing.T) {
	t.Parallel()
	srv := serve(t, []byte(csvWith(8, "CVE-2026-9001,abc,0.5", "CVE-2026-9002,0.5,1.5")))

	_, err := newFeed(srv.URL + "/epss.csv").Refresh(t.Context())
	if err == nil {
		t.Fatal("Refresh() with 2 bad rows of 10 = nil error, want one")
	}
	if got := err.Error(); !strings.HasPrefix(got, "epss: decode "+srv.URL+"/epss.csv: ") || !strings.Contains(got, "2 of 10") {
		t.Errorf("Refresh() error = %q, want it to name the feed, URL and 2 of 10 rows", got)
	}
}

func TestEPSSRefresh_SkippedRowsWithinLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		bad  string
	}{
		{name: "malformed score", bad: "CVE-2026-9001,abc,0.5"},
		{name: "malformed percentile", bad: "CVE-2026-9001,0.5,"},
		{name: "negative score", bad: "CVE-2026-9001,-0.1,0.5"},
		{name: "percentile above one", bad: "CVE-2026-9001,0.5,1.0001"},
		{name: "NaN score", bad: "CVE-2026-9001,NaN,0.5"},
		{name: "too few fields", bad: "CVE-2026-9001,0.5"},
		{name: "too many fields", bad: "CVE-2026-9001,0.5,0.5,0.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// 1 bad row of 100 is exactly 1%, which is allowed.
			srv := serve(t, []byte(csvWith(99, tt.bad)))
			got, err := newFeed(srv.URL).Refresh(t.Context())
			if err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if len(got) != 99 {
				t.Errorf("Refresh() returned %d signals, want 99", len(got))
			}
		})
	}
}

func TestEPSSRefresh_NoHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "rows without header", body: "#model_version:v2025.03.14,score_date:2026-11-19T12:55:00Z\nCVE-2026-0001,0.5,0.5\n"},
		{name: "comment only", body: "#model_version:v2025.03.14,score_date:2026-11-19T12:55:00Z\n"},
		{name: "empty body", body: ""},
		{name: "wrong header", body: "cve,score,percentile\nCVE-2026-0001,0.5,0.5\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := serve(t, []byte(tt.body))
			_, err := newFeed(srv.URL).Refresh(t.Context())
			if err == nil {
				t.Fatal("Refresh() = nil error, want one")
			}
			if !strings.Contains(err.Error(), "missing header") {
				t.Errorf("Refresh() error = %q, want it to report the missing header", err)
			}
		})
	}
}

func TestEPSSRefresh_HeaderOnly(t *testing.T) {
	t.Parallel()
	srv := serve(t, []byte("cve,epss,percentile\n"))

	got, err := newFeed(srv.URL).Refresh(t.Context())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Refresh() = %+v, want no signals", got)
	}
}

func TestEPSSRefresh_BadGzip(t *testing.T) {
	t.Parallel()
	srv := serve(t, []byte{0x1f, 0x8b, 0x00, 0x01, 0x02})

	if _, err := newFeed(srv.URL).Refresh(t.Context()); err == nil {
		t.Fatal("Refresh() of a corrupt gzip body = nil error, want one")
	}
}

func TestEPSSRefresh_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newFeed(srv.URL + "/epss.csv.gz").Refresh(t.Context())
	if err == nil {
		t.Fatal("Refresh() = nil error, want one")
	}
	got := err.Error()
	if !strings.HasPrefix(got, "epss: fetch "+srv.URL+"/epss.csv.gz: ") || !strings.Contains(got, "404") {
		t.Errorf("Refresh() error = %q, want it to name the feed, URL and status 404", got)
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

			err := newFeed(srv.URL + "/epss.csv.gz").Check(t.Context())
			host := strings.TrimPrefix(srv.URL, "http://")
			switch {
			case tt.wantErr && err == nil:
				t.Fatal("Check() = nil, want an error")
			case tt.wantErr && !strings.Contains(err.Error(), host):
				t.Errorf("Check() = %q, want it to name %s", err, host)
			case !tt.wantErr && err != nil:
				t.Fatalf("Check() = %v, want nil", err)
			}
			if gotMethod != http.MethodHead || gotPath != "/epss.csv.gz" {
				t.Errorf("request = %s %s, want HEAD /epss.csv.gz", gotMethod, gotPath)
			}
		})
	}
}

func TestCheck_Redirect(t *testing.T) {
	t.Parallel()
	var gotMethod string
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
	}))
	defer b.Close()
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.URL+"/epss.csv.gz", http.StatusMovedPermanently)
	}))
	defer a.Close()

	if err := newFeed(a.URL + "/epss.csv.gz").Check(t.Context()); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
	if gotMethod != http.MethodHead {
		t.Errorf("redirected request method = %q, want HEAD", gotMethod)
	}
}
