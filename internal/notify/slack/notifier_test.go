package slack_test

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
	"github.com/Amrit-Code/upstreamwatch/internal/notify/slack"
	"github.com/Amrit-Code/upstreamwatch/internal/report"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var (
	_ report.Notifier = (*slack.Notifier)(nil)
	_ checker         = (*slack.Notifier)(nil)
)

// webhookPath stands in for the secret part of a webhook URL.
const webhookPath = "/services/T000/B000/example-secret-token"

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

// webhook is a fake Slack webhook that records requests and answers each
// with status.
type webhook struct {
	srv    *httptest.Server
	status int

	mu   sync.Mutex
	reqs []request
}

func newWebhook(t *testing.T, status int) *webhook {
	t.Helper()
	w := &webhook{status: status}
	w.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		w.mu.Lock()
		w.reqs = append(w.reqs, request{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: b})
		w.mu.Unlock()
		rw.WriteHeader(w.status)
		_, _ = io.WriteString(rw, "ok")
	}))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *webhook) requests() []request {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]request(nil), w.reqs...)
}

func newNotifier(t *testing.T, w *webhook, logs io.Writer) *slack.Notifier {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}, MaxWait: time.Millisecond}, logger)
	n, err := slack.New(hc, model.Secret(w.srv.URL+webhookPath), logger)
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

// manyItemDigest has 22 items, the first with an injected link and
// ampersands, plus one more left out by the digest job.
func manyItemDigest() model.Digest {
	d := model.Digest{
		GeneratedAt: generatedAt(),
		MoreChanged: 1,
		Open:        23,
		Acked:       1,
		FindingsURL: "https://upw.example.com/",
	}
	d.New = append(d.New, model.DigestItem{
		FindingID: 1, Kind: model.KindAdvisory, Tag: "[A]",
		Headline: "high  GHSA-xxxx  <https://evil|click> & <!channel>",
		Lines:    []string{"R&D *note* _here_", "a > b < c"},
		URL:      "https://upw.example.com/f/1",
	})
	for i := 2; i <= 18; i++ {
		d.New = append(d.New, model.DigestItem{
			FindingID: int64(i), Kind: model.KindAdvisory, Tag: "[A]",
			Headline: "medium  GO-2026-" + strconv.Itoa(4000+i) + "  pkg:golang/example.com/lib 1.0.0",
			Lines:    []string{"fixed in 1.0.1"},
			URL:      "https://upw.example.com/f/" + strconv.Itoa(i),
		})
	}
	for i := 19; i <= 22; i++ {
		d.Changed = append(d.Changed, model.DigestItem{
			FindingID: int64(i), Kind: model.KindIssue, Tag: "[I]",
			Headline: "affects (0.82)  example-org/example-lib#" + strconv.Itoa(i),
		})
	}
	return d
}

type sentMessage struct {
	Text   string `json:"text"`
	Blocks []struct {
		Type string `json:"type"`
		Text *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"text"`
		Elements []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"elements"`
	} `json:"blocks"`
}

func decode(t *testing.T, body []byte) sentMessage {
	t.Helper()
	var m sentMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, body)
	}
	return m
}

func TestSlackSend_Golden(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusOK)
	var logs syncBuffer
	n := newNotifier(t, w, &logs)

	if err := n.Send(t.Context(), manyItemDigest()); err != nil {
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
	golden(t, "TestSlackSend_Golden", r.body)

	m := decode(t, r.body)
	if len(m.Blocks) != 22 {
		t.Errorf("got %d blocks, want 22 (header, 20 sections, context)", len(m.Blocks))
	}
	first := m.Blocks[1].Text.Text
	want := "*[A] high  GHSA-xxxx  &lt;https://evil|click&gt; &amp; &lt;!channel&gt;*\nR&amp;D *note* _here_\na &gt; b &lt; c\n<https://upw.example.com/f/1|Open>"
	if first != want {
		t.Errorf("first section = %q\nwant            %q", first, want)
	}
	ctxBlock := m.Blocks[len(m.Blocks)-1]
	if ctxBlock.Type != "context" || len(ctxBlock.Elements) != 2 || ctxBlock.Elements[1].Text != "… and 3 more https://upw.example.com/" {
		t.Errorf("last block = %+v, want a context block ending with \"… and 3 more\"", ctxBlock)
	}

	l := logs.String()
	if !strings.Contains(l, `msg="digest sent" channel=slack host=`+w.srv.Listener.Addr().String()+" items=22") {
		t.Errorf("logs lack the send line:\n%s", l)
	}
	if strings.Contains(l, "example-secret-token") {
		t.Errorf("logs contain the webhook path:\n%s", l)
	}
}

func TestSlackSend_Heartbeat(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusOK)
	n := newNotifier(t, w, io.Discard)
	d := model.Digest{GeneratedAt: generatedAt(), Heartbeat: true, Open: 3}

	if err := n.Send(t.Context(), d); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	want := `{"text":"upstreamwatch — 2026-11-18 — all quiet","blocks":[` +
		`{"type":"header","text":{"type":"plain_text","text":"upstreamwatch — 2026-11-18 — all quiet"}},` +
		`{"type":"context","elements":[{"type":"mrkdwn","text":"Open: 3, acked: 0, snoozed: 0."}]}]}`
	if got := string(w.requests()[0].body); got != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
}

func TestSlackSend_StatusError(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusInternalServerError)
	var logs syncBuffer
	n := newNotifier(t, w, &logs)

	err := n.Send(t.Context(), manyItemDigest())
	if err == nil {
		t.Fatal("Send() error = nil, want an error")
	}
	host := w.srv.Listener.Addr().String()
	if want := "slack: post to " + host + ": status 500"; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Send() error = %q, want prefix %q", err, want)
	}
	if strings.Contains(err.Error(), "/services") || strings.Contains(err.Error(), "example-secret-token") {
		t.Errorf("Send() error %q names the webhook path", err)
	}
	if se, ok := errors.AsType[*httpx.StatusError](err); !ok || se.Status != http.StatusInternalServerError {
		t.Errorf("Send() error does not wrap *httpx.StatusError with status 500: %v", err)
	}
	if strings.Contains(logs.String(), "example-secret-token") {
		t.Errorf("logs contain the webhook path:\n%s", logs.String())
	}
}

func TestSlackCheck(t *testing.T) {
	t.Parallel()
	w := newWebhook(t, http.StatusOK)
	n := newNotifier(t, w, io.Discard)

	if err := n.Check(t.Context()); err != nil {
		t.Errorf("Check() error = %v, want nil", err)
	}
	if got := len(w.requests()); got != 0 {
		t.Errorf("Check() made %d requests, want 0", got)
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
		{name: "https", url: "https://hooks.example.com" + webhookPath},
		{name: "http loopback address", url: "http://[::1]:8080" + webhookPath},
		{name: "http other host", url: "http://hooks.example.com" + webhookPath, wantErr: true},
		{name: "relative", url: webhookPath, wantErr: true},
		{name: "empty", url: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := slack.New(hc, model.Secret(tt.url), slog.New(slog.DiscardHandler))
			if (err != nil) != tt.wantErr {
				t.Fatalf("New(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "example-secret-token") {
				t.Errorf("New() error %q names the webhook path", err)
			}
		})
	}
}

func TestSlackSend_SectionCap(t *testing.T) {
	t.Parallel()
	const findingURL = "https://upw.example.com/f/1"
	tests := []struct {
		name       string
		item       model.DigestItem
		wantPrefix string
		wantSuffix string
	}{
		{
			name:       "long lines are cut, headline and link kept",
			item:       model.DigestItem{Tag: "[A]", Headline: "high  GO-2026-4866", Lines: []string{strings.Repeat("é", 5000)}, URL: findingURL},
			wantPrefix: "*[A] high  GO-2026-4866*\né",
			wantSuffix: "é…\n<" + findingURL + "|Open>",
		},
		{
			name:       "headline too long on its own",
			item:       model.DigestItem{Tag: "[A]", Headline: strings.Repeat("x", 4000), Lines: []string{"detail"}, URL: findingURL},
			wantPrefix: "*[A] xxx",
			wantSuffix: "x…",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := newWebhook(t, http.StatusOK)
			n := newNotifier(t, w, io.Discard)
			d := model.Digest{GeneratedAt: generatedAt(), New: []model.DigestItem{tt.item}}

			if err := n.Send(t.Context(), d); err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			got := decode(t, w.requests()[0].body).Blocks[1].Text.Text
			if runes := utf8.RuneCountInString(got); runes != 2900 {
				t.Errorf("section text is %d runes, want 2900", runes)
			}
			if !utf8.ValidString(got) {
				t.Error("section text is not valid UTF-8")
			}
			if !strings.HasPrefix(got, tt.wantPrefix) || !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("section text = %.40q…%q, want prefix %q and suffix %q", got, got[max(0, len(got)-40):], tt.wantPrefix, tt.wantSuffix)
			}
		})
	}
}
