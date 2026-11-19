package github_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/issues/github"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var _ checker = (*github.Source)(nil)

// testToken is a made-up credential; only its last four characters may
// appear in errors.
const testToken = "example-token-value-1234"

// newSource returns a github.Source whose httpx client reaches loopback test
// servers and retries quickly.
func newSource(baseURL string, token model.Secret) *github.Source {
	logger := slog.New(slog.DiscardHandler)
	hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}, MaxWait: time.Millisecond}, logger)
	return github.New(hc, token, github.Options{BaseURL: baseURL}, logger)
}

// recorder is a handler that records the last request's headers and answers
// with status.
type recorder struct {
	status int

	mu     sync.Mutex
	method string
	path   string
	header http.Header
}

func (h *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.method, h.path, h.header = r.Method, r.URL.Path, r.Header.Clone()
	h.mu.Unlock()
	w.WriteHeader(h.status)
}

func TestSource_Check_Headers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		token    model.Secret
		wantAuth string
	}{
		{name: "with token", token: testToken, wantAuth: "Bearer " + testToken},
		{name: "without token", token: "", wantAuth: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{status: http.StatusOK}
			srv := httptest.NewServer(rec)
			defer srv.Close()

			if err := newSource(srv.URL, tt.token).Check(t.Context()); err != nil {
				t.Fatalf("Check() = %v, want nil", err)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.method != http.MethodGet || rec.path != "/rate_limit" {
				t.Errorf("request = %s %s, want GET /rate_limit", rec.method, rec.path)
			}
			_, sent := rec.header["Authorization"]
			if got := rec.header.Get("Authorization"); got != tt.wantAuth || sent != (tt.wantAuth != "") {
				t.Errorf("Authorization = %q (sent %v), want %q", got, sent, tt.wantAuth)
			}
			if got, want := rec.header.Get("Accept"), "application/vnd.github+json"; got != want {
				t.Errorf("Accept = %q, want %q", got, want)
			}
			if got, want := rec.header.Get("X-GitHub-Api-Version"), "2022-11-28"; got != want {
				t.Errorf("X-GitHub-Api-Version = %q, want %q", got, want)
			}
		})
	}
}

func TestSource_Check_Unauthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(&recorder{status: http.StatusUnauthorized})
	defer srv.Close()

	err := newSource(srv.URL, testToken).Check(t.Context())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}
	got := err.Error()
	host := strings.TrimPrefix(srv.URL, "http://")
	if want := host + ": token ****1234 rejected (401)"; got != want {
		t.Errorf("Check() = %q, want %q", got, want)
	}
	if strings.Contains(got, testToken) {
		t.Errorf("Check() = %q, contains the token", got)
	}
}

func TestSource_Check_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(&recorder{status: http.StatusInternalServerError})
	defer srv.Close()

	err := newSource(srv.URL, testToken).Check(t.Context())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}
	got := err.Error()
	host := strings.TrimPrefix(srv.URL, "http://")
	if !strings.Contains(got, host) || !strings.Contains(got, "500") || !strings.Contains(got, "****1234") {
		t.Errorf("Check() = %q, want it to name %s, status 500 and the redacted token", got, host)
	}
	if strings.Contains(got, testToken) {
		t.Errorf("Check() = %q, contains the token", got)
	}
}

func TestSource_Check_ServerDown(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	err := newSource(url, "").Check(t.Context())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}
	if host := strings.TrimPrefix(url, "http://"); !strings.Contains(err.Error(), host) {
		t.Errorf("Check() = %q, want it to name %s", err, host)
	}
}
