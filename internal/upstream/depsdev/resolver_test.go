package depsdev_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev"
)

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var _ checker = (*depsdev.Resolver)(nil)

// newResolver returns a depsdev.Resolver whose httpx client reaches loopback
// test servers and retries quickly.
func newResolver(baseURL string) *depsdev.Resolver {
	logger := slog.New(slog.DiscardHandler)
	hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}, MaxWait: time.Millisecond}, logger)
	return depsdev.New(hc, depsdev.Options{BaseURL: baseURL}, logger)
}

func TestResolver_Check_OK(t *testing.T) {
	t.Parallel()
	var gotMethod, gotRawPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotRawPath, gotAuth = r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"versionKey":{"system":"GO","name":"github.com/google/uuid","version":"v1.6.0"}}`))
	}))
	defer srv.Close()

	if err := newResolver(srv.URL).Check(t.Context()); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
	want := "/v3/systems/go/packages/github.com%2Fgoogle%2Fuuid/versions/v1.6.0"
	if gotMethod != http.MethodGet || gotRawPath != want {
		t.Errorf("request = %s %s, want GET %s", gotMethod, gotRawPath, want)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none", gotAuth)
	}
}

func TestResolver_Check_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := newResolver(srv.URL).Check(t.Context())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	if got := err.Error(); !strings.Contains(got, host) || !strings.Contains(got, "500") {
		t.Errorf("Check() = %q, want it to name %s and status 500", got, host)
	}
}

func TestResolver_Check_ServerDown(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	err := newResolver(url).Check(t.Context())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}
	if host := strings.TrimPrefix(url, "http://"); !strings.Contains(err.Error(), host) {
		t.Errorf("Check() = %q, want it to name %s", err, host)
	}
}
