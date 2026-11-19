package osv_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var _ checker = (*osv.Client)(nil)

// newClient returns an osv.Client whose httpx client reaches loopback test
// servers and retries quickly.
func newClient(baseURL string) *osv.Client {
	logger := slog.New(slog.DiscardHandler)
	hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}, MaxWait: time.Millisecond}, logger)
	return osv.New(hc, osv.Options{BaseURL: baseURL}, logger)
}

func TestClient_Check_OK(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"GO-2020-0001"}`))
	}))
	defer srv.Close()

	if err := newClient(srv.URL).Check(t.Context()); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/vulns/GO-2020-0001" {
		t.Errorf("request = %s %s, want GET /v1/vulns/GO-2020-0001", gotMethod, gotPath)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none", gotAuth)
	}
}

func TestClient_Check_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := newClient(srv.URL).Check(t.Context())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	if got := err.Error(); !strings.Contains(got, host) || !strings.Contains(got, "500") {
		t.Errorf("Check() = %q, want it to name %s and status 500", got, host)
	}
}

func TestClient_Check_ServerDown(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	err := newClient(url).Check(t.Context())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}
	if host := strings.TrimPrefix(url, "http://"); !strings.Contains(err.Error(), host) {
		t.Errorf("Check() = %q, want it to name %s", err, host)
	}
}
