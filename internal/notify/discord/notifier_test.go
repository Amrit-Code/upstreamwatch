package discord_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/discord"
	"github.com/Amrit-Code/upstreamwatch/internal/report"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var (
	_ report.Notifier = (*discord.Notifier)(nil)
	_ checker         = (*discord.Notifier)(nil)
)

// webhookPath stands in for the secret part of a webhook URL.
const webhookPath = "/api/webhooks/123456/example-secret-token"

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

// request is what the fake webhook received.
type request struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// webhook is a fake Discord webhook that records requests and answers each
// with status and body.
type webhook struct {
	srv    *httptest.Server
	status int
	body   string

	mu   sync.Mutex
	reqs []request
}

func newWebhook(t *testing.T, status int, body string) *webhook {
	t.Helper()
	w := &webhook{status: status, body: body}
	w.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		w.mu.Lock()
		w.reqs = append(w.reqs, request{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: b})
		w.mu.Unlock()
		rw.WriteHeader(w.status)
		_, _ = io.WriteString(rw, w.body)
	}))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *webhook) requests() []request {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]request(nil), w.reqs...)
}

// newNotifier returns a Notifier for the fake webhook whose logs, and those
// of its httpx client, go to logs.
func newNotifier(t *testing.T, w *webhook, logs io.Writer) *discord.Notifier {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}, MaxWait: time.Millisecond}, logger)
	n, err := discord.New(hc, model.Secret(w.srv.URL+webhookPath), logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return n
}

// golden compares got, indented, with testdata/<name>.golden, rewriting the
// file under -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	var ind bytes.Buffer
	if err := json.Indent(&ind, got, "", "  "); err != nil {
		t.Fatalf("request body is not JSON: %v\n%s", err, got)
	}
	ind.WriteByte('\n')
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, ind.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file: %v (run with -update to create it)", err)
	}
	if !bytes.Equal(ind.Bytes(), want) {
		t.Errorf("request body differs from %s:\ngot:\n%s\nwant:\n%s", path, ind.Bytes(), want)
	}
}

func generatedAt() time.Time { return time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC) }

// twelveItemDigest has 12 items, one of them with a mention and a markdown
// link in its headline, plus one more left out by the digest job.
func twelveItemDigest() model.Digest {
	d := model.Digest{
		GeneratedAt: generatedAt(),
		MoreNew:     1,
		Open:        11,
		Acked:       4,
		Snoozed:     2,
		FindingsURL: "https://upw.example.com/",
	}
	d.New = append(d.New, model.DigestItem{
		FindingID: 1, Kind: model.KindAdvisory, Tag: "[A]",
		Headline: "high  GO-2026-4866  pkg:golang/toolchain 1.26.0  (repo: server)",
		Lines:    []string{"net/http: request smuggling via chunked_encoding ... fixed in 1.26.4.", "KEV: no. EPSS 0.12 (p71)."},
		URL:      "https://upw.example.com/f/1",
	}, model.DigestItem{
		FindingID: 2, Kind: model.KindAdvisory, Tag: "[A]",
		Headline: "critical  GHSA-xxxx  @everyone see [x](http://evil)",
		Lines:    []string{"*bold* _it_ ~strike~ `code` |spoiler| > quote \\ back <@123456789012345678> <t:1700000000>"},
		URL:      "https://upw.example.com/f/2",
	})
	for i := 3; i <= 8; i++ {
		d.New = append(d.New, model.DigestItem{
			FindingID: int64(i), Kind: model.KindAdvisory, Tag: "[A]",
			Headline: "medium  GO-2026-" + strconv.Itoa(4000+i) + "  pkg:golang/example.com/lib 1.0.0",
			Lines:    []string{"Exposed since 2026-09-14 — confidence high"},
			URL:      "https://upw.example.com/f/" + strconv.Itoa(i),
		})
	}
	for i := 9; i <= 12; i++ {
		d.Changed = append(d.Changed, model.DigestItem{
			FindingID: int64(i), Kind: model.KindAdvisory, Tag: "[A]",
			Headline: "low  PYSEC-2026-" + strconv.Itoa(2000+i) + "  requests 2.31.0",
			Lines:    []string{"now has a fixed version (2.32.5)"},
		})
	}
	return d
}

type sentMessage struct {
	Content string `json:"content"`
	Embeds  []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
	} `json:"embeds"`
	AllowedMentions *struct {
		Parse []string `json:"parse"`
	} `json:"allowed_mentions"`
}

func decode(t *testing.T, body []byte) sentMessage {
	t.Helper()
	var m sentMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, body)
	}
	return m
}

func TestDiscordSend_Golden(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusNoContent, "")
	var logs syncBuffer
	n := newNotifier(t, w, &logs)

	if err := n.Send(t.Context(), twelveItemDigest()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	reqs := w.requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if r.method != http.MethodPost || r.path != webhookPath {
		t.Errorf("request = %s %s, want POST %s", r.method, r.path, webhookPath)
	}
	if got := r.header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	golden(t, "TestDiscordSend_Golden", r.body)

	m := decode(t, r.body)
	if len(m.Embeds) != 10 {
		t.Errorf("got %d embeds, want 10", len(m.Embeds))
	}
	if m.AllowedMentions == nil || m.AllowedMentions.Parse == nil || len(m.AllowedMentions.Parse) != 0 {
		t.Errorf("allowed_mentions = %+v, want parse: []", m.AllowedMentions)
	}
	if !strings.Contains(m.Content, "\n… and 3 more https://upw.example.com/") {
		t.Errorf("content = %q, want an \"… and 3 more\" line with the findings URL", m.Content)
	}
	title := m.Embeds[1].Title
	if strings.Contains(title, "@everyone") || !strings.Contains(title, "@\u200beveryone") {
		t.Errorf("title %q: mention not neutralised", title)
	}
	if !strings.Contains(title, `\[x\](http://evil)`) {
		t.Errorf("title %q: link markdown not neutralised", title)
	}
	if desc := m.Embeds[1].Description; !strings.Contains(desc, "\\<@\u200b123456789012345678\\> \\<t:1700000000\\>") {
		t.Errorf("description %q: '<' markup not neutralised", desc)
	}

	l := logs.String()
	if !strings.Contains(l, `msg="digest sent" channel=discord host=`+w.srv.Listener.Addr().String()+" items=12") {
		t.Errorf("logs lack the send line:\n%s", l)
	}
	if strings.Contains(l, "example-secret-token") {
		t.Errorf("logs contain the webhook path:\n%s", l)
	}
}

func TestDiscordSend_Heartbeat(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusNoContent, "")
	n := newNotifier(t, w, io.Discard)
	d := model.Digest{
		GeneratedAt: generatedAt(),
		Heartbeat:   true,
		LastRuns: []model.Run{
			{Job: "ingest", Status: model.RunSucceeded, StartedAt: generatedAt().Add(-time.Hour), FinishedAt: generatedAt().Add(-55 * time.Minute)},
		},
		Open:        3,
		FindingsURL: "https://upw.example.com/",
	}

	if err := n.Send(t.Context(), d); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	reqs := w.requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	want := `{"content":"upstreamwatch — 2026-11-18 — all quiet\nLast runs: ingest 07:05 ok.\nOpen: 3, acked: 0, snoozed: 0.  https://upw.example.com/","allowed_mentions":{"parse":[]}}`
	if got := string(reqs[0].body); got != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
}

func TestDiscordSend_EmbedBudget(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusNoContent, "")
	n := newNotifier(t, w, io.Discard)
	d := model.Digest{GeneratedAt: generatedAt()}
	for i := range 10 {
		d.New = append(d.New, model.DigestItem{Tag: "[A]", Headline: "item " + strconv.Itoa(i), Lines: []string{strings.Repeat("x", 1000)}})
	}

	if err := n.Send(t.Context(), d); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	m := decode(t, w.requests()[0].body)
	total := 0
	for _, e := range m.Embeds {
		total += utf8.RuneCountInString(e.Title) + utf8.RuneCountInString(e.Description)
	}
	if total >= 5500 {
		t.Errorf("embed characters = %d, want under 5500", total)
	}
	if len(m.Embeds) != 5 {
		t.Errorf("got %d embeds, want 5", len(m.Embeds))
	}
	if !strings.HasSuffix(m.Content, "\n… and 5 more") {
		t.Errorf("content = %q, want an \"… and 5 more\" line", m.Content)
	}
}

func TestDiscordSend_Truncation(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusNoContent, "")
	n := newNotifier(t, w, io.Discard)
	d := model.Digest{GeneratedAt: generatedAt(), New: []model.DigestItem{{
		Tag:      "[A]",
		Headline: strings.Repeat("h", 300),
		Lines:    []string{strings.Repeat("*", 999)},
	}}}

	if err := n.Send(t.Context(), d); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	e := decode(t, w.requests()[0].body).Embeds[0]
	if want := "[A] " + strings.Repeat("h", 251) + "…"; e.Title != want {
		t.Errorf("title = %q (%d runes), want %d runes ending in …", e.Title, utf8.RuneCountInString(e.Title), utf8.RuneCountInString(want))
	}
	// Each "*" escapes to two runes; the cut never separates a backslash
	// from its character.
	if want := strings.Repeat(`\*`, 499) + "…"; e.Description != want {
		t.Errorf("description = %q, want 499 escaped stars and …", e.Description)
	}
}

func TestDiscordSend_StatusError(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusInternalServerError, "upstream broke")
	var logs syncBuffer
	n := newNotifier(t, w, &logs)

	err := n.Send(t.Context(), twelveItemDigest())
	if err == nil {
		t.Fatal("Send() error = nil, want an error")
	}
	host := w.srv.Listener.Addr().String()
	if want := "discord: post to " + host + ": status 500"; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Send() error = %q, want prefix %q", err, want)
	}
	if strings.Contains(err.Error(), "/api/webhooks") || strings.Contains(err.Error(), "example-secret-token") {
		t.Errorf("Send() error %q names the webhook path", err)
	}
	if se, ok := errors.AsType[*httpx.StatusError](err); !ok || se.Status != http.StatusInternalServerError {
		t.Errorf("Send() error does not wrap *httpx.StatusError with status 500: %v", err)
	}
	if strings.Contains(logs.String(), "example-secret-token") {
		t.Errorf("logs contain the webhook path:\n%s", logs.String())
	}
	if got := len(w.requests()); got != 1 {
		t.Errorf("got %d requests, want 1 (POST is not retried)", got)
	}
}

func TestDiscordCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "webhook exists", status: http.StatusOK},
		{name: "webhook unknown", status: http.StatusNotFound, wantErr: true},
		{name: "token invalid", status: http.StatusUnauthorized, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := newWebhook(t, tt.status, `{"type":1,"id":"123456","name":"upw"}`)
			n := newNotifier(t, w, io.Discard)

			err := n.Check(t.Context())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Check() error = %v, wantErr %v", err, tt.wantErr)
			}
			reqs := w.requests()
			if len(reqs) != 1 || reqs[0].method != http.MethodGet || reqs[0].path != webhookPath {
				t.Errorf("requests = %+v, want one GET %s", reqs, webhookPath)
			}
			if err != nil {
				want := "discord: check " + w.srv.Listener.Addr().String() + ": status " + strconv.Itoa(tt.status)
				if err.Error() != want {
					t.Errorf("Check() error = %q, want %q", err, want)
				}
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
		{name: "https", url: "https://discord.example.com" + webhookPath},
		{name: "http loopback address", url: "http://127.0.0.1:8080" + webhookPath},
		{name: "http localhost", url: "http://localhost" + webhookPath},
		{name: "http other host", url: "http://discord.example.com" + webhookPath, wantErr: true},
		{name: "relative", url: webhookPath, wantErr: true},
		{name: "other scheme", url: "ftp://discord.example.com" + webhookPath, wantErr: true},
		{name: "empty", url: "", wantErr: true},
		{name: "unparseable", url: "https://discord.example.com/%zz/example-secret-token", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n, err := discord.New(hc, model.Secret(tt.url), slog.New(slog.DiscardHandler))
			if (err != nil) != tt.wantErr {
				t.Fatalf("New(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "example-secret-token") {
				t.Errorf("New() error %q names the webhook path", err)
			}
			if err == nil && n == nil {
				t.Error("New() = nil, nil")
			}
		})
	}
}

func TestDiscordSend_ItemWithoutLines(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusNoContent, "")
	n := newNotifier(t, w, io.Discard)
	d := model.Digest{GeneratedAt: generatedAt(), New: []model.DigestItem{{Tag: "[J]", Headline: "job vulns failed"}}}

	if err := n.Send(t.Context(), d); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	body := w.requests()[0].body
	if bytes.Contains(body, []byte(`"description"`)) {
		t.Errorf("body = %s, want no description for an item without lines", body)
	}
	if got := decode(t, body).Embeds[0].Title; got != "[J] job vulns failed" {
		t.Errorf("title = %q, want %q", got, "[J] job vulns failed")
	}
}
