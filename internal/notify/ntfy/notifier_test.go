package ntfy_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy"
	"github.com/Amrit-Code/upstreamwatch/internal/report"
)

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var (
	_ report.Notifier = (*ntfy.Notifier)(nil)
	_ checker         = (*ntfy.Notifier)(nil)
)

// topicPath stands in for the secret topic name.
const topicPath = "/example-secret-topic"

// syncBuffer is a bytes.Buffer safe for the logger to write from any
// goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// request is what the fake server received.
type request struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// server is a fake ntfy server that records requests and answers each with
// status and body.
type server struct {
	srv    *httptest.Server
	status int
	body   string

	mu   sync.Mutex
	reqs []request
}

func newServer(t *testing.T, status int, body string) *server {
	t.Helper()
	s := &server{status: status, body: body}
	s.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		s.mu.Lock()
		s.reqs = append(s.reqs, request{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: b})
		s.mu.Unlock()
		rw.WriteHeader(s.status)
		_, _ = io.WriteString(rw, s.body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *server) requests() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request(nil), s.reqs...)
}

func newNotifier(t *testing.T, s *server, logs io.Writer) *ntfy.Notifier {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}, MaxWait: time.Millisecond}, logger)
	n, err := ntfy.New(hc, model.Secret(s.srv.URL+topicPath), logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return n
}

func generatedAt() time.Time { return time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC) }

// longDigest has one item whose detail line of three-byte runes pushes the
// plain text past 4,000 bytes; pad shifts where the cut lands.
func longDigest(pad int) model.Digest {
	return model.Digest{
		GeneratedAt: generatedAt(),
		New: []model.DigestItem{{
			FindingID: 1, Kind: model.KindAdvisory, Tag: "[A]",
			Headline: "high  GO-2026-4866" + strings.Repeat("x", pad),
			Lines:    []string{strings.Repeat("—", 1500)},
			URL:      "https://upw.example.com/f/1",
		}},
		Open:        1,
		FindingsURL: "https://upw.example.com/",
	}
}

func TestNtfySend(t *testing.T) {
	t.Parallel()
	for _, pad := range []int{0, 1, 2} {
		t.Run("pad "+strconv.Itoa(pad), func(t *testing.T) {
			t.Parallel()
			s := newServer(t, http.StatusOK, `{"id":"abc"}`)
			var logs syncBuffer
			n := newNotifier(t, s, &logs)
			d := longDigest(pad)

			if err := n.Send(t.Context(), d); err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			reqs := s.requests()
			if len(reqs) != 1 {
				t.Fatalf("got %d requests, want 1", len(reqs))
			}
			r := reqs[0]
			if r.method != http.MethodPost || r.path != topicPath {
				t.Errorf("request = %s %s, want POST %s", r.method, r.path, topicPath)
			}
			wantHeaders := map[string]string{
				"Title":    "upstreamwatch - 2026-11-18 - 1 new, 0 changed",
				"Click":    "https://upw.example.com/",
				"Tags":     "warning",
				"Priority": "default",
			}
			for k, want := range wantHeaders {
				if got := r.header.Get(k); got != want {
					t.Errorf("header %s = %q, want %q", k, got, want)
				}
			}

			body := string(r.body)
			if len(body) > 4000 || len(body) < 4000-5 {
				t.Errorf("body is %d bytes, want 3995 to 4000", len(body))
			}
			if !utf8.ValidString(body) {
				t.Error("body is not valid UTF-8")
			}
			cut, ok := strings.CutSuffix(body, "…")
			if !ok || !strings.HasPrefix(d.PlainText(), cut) {
				t.Errorf("body is not a prefix of the plain text followed by …")
			}

			l := logs.String()
			if !strings.Contains(l, `msg="digest sent" channel=ntfy host=`+s.srv.Listener.Addr().String()+" items=1") {
				t.Errorf("logs lack the send line:\n%s", l)
			}
			if strings.Contains(l, "example-secret-topic") {
				t.Errorf("logs contain the topic:\n%s", l)
			}
		})
	}
}

func TestNtfySend_Heartbeat(t *testing.T) {
	t.Parallel()
	s := newServer(t, http.StatusOK, `{"id":"abc"}`)
	n := newNotifier(t, s, io.Discard)
	d := model.Digest{GeneratedAt: generatedAt(), Heartbeat: true, Open: 2}

	if err := n.Send(t.Context(), d); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	r := s.requests()[0]
	if got := string(r.body); got != d.PlainText() {
		t.Errorf("body = %q, want the plain text %q", got, d.PlainText())
	}
	if got := r.header.Get("Tags"); got != "white_check_mark" {
		t.Errorf("Tags = %q, want white_check_mark", got)
	}
	if got := r.header.Get("Title"); got != "upstreamwatch - 2026-11-18 - all quiet" {
		t.Errorf("Title = %q", got)
	}
	if _, ok := r.header["Click"]; ok {
		t.Errorf("Click header sent without a findings URL")
	}
}

func TestNtfySend_StatusError(t *testing.T) {
	t.Parallel()
	s := newServer(t, http.StatusInternalServerError, "broken")
	var logs syncBuffer
	n := newNotifier(t, s, &logs)

	err := n.Send(t.Context(), longDigest(0))
	if err == nil {
		t.Fatal("Send() error = nil, want an error")
	}
	host := s.srv.Listener.Addr().String()
	if want := "ntfy: post to " + host + ": status 500"; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Send() error = %q, want prefix %q", err, want)
	}
	if strings.Contains(err.Error(), "example-secret-topic") {
		t.Errorf("Send() error %q names the topic", err)
	}
	if se, ok := errors.AsType[*httpx.StatusError](err); !ok || se.Status != http.StatusInternalServerError {
		t.Errorf("Send() error does not wrap *httpx.StatusError with status 500: %v", err)
	}
	if strings.Contains(logs.String(), "example-secret-topic") {
		t.Errorf("logs contain the topic:\n%s", logs.String())
	}
}

func TestNtfyCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "healthy", status: http.StatusOK, body: `{"healthy":true}`},
		{name: "unhealthy", status: http.StatusOK, body: `{"healthy":false}`, wantErr: "server reports unhealthy"},
		{name: "field missing", status: http.StatusOK, body: `{}`, wantErr: "server reports unhealthy"},
		{name: "not JSON", status: http.StatusOK, body: `ok`, wantErr: "decode health answer"},
		{name: "not found", status: http.StatusNotFound, body: ``, wantErr: "status 404"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newServer(t, tt.status, tt.body)
			n := newNotifier(t, s, io.Discard)

			err := n.Check(t.Context())
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Check() error = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("Check() error = %v, want one containing %q", err, tt.wantErr)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "ntfy: check "+s.srv.Listener.Addr().String()+": ") {
				t.Errorf("Check() error = %q, want it to name the host", err)
			}
			reqs := s.requests()
			if len(reqs) != 1 || reqs[0].method != http.MethodGet || reqs[0].path != "/v1/health" {
				t.Errorf("requests = %+v, want one GET /v1/health", reqs)
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()
	hc := httpx.New(nil, httpx.Options{}, slog.New(slog.DiscardHandler))
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "https", url: "https://ntfy.example.com" + topicPath},
		{name: "http loopback", url: "http://127.0.0.1:2586" + topicPath},
		{name: "http private address", url: "http://192.168.1.10" + topicPath, wantErr: true},
		{name: "relative", url: topicPath, wantErr: true},
		{name: "empty", url: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ntfy.New(hc, model.Secret(tt.url), slog.New(slog.DiscardHandler))
			if (err != nil) != tt.wantErr {
				t.Fatalf("New(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "example-secret-topic") {
				t.Errorf("New() error %q names the topic", err)
			}
		})
	}
}
