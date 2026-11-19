package email_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/email"
	"github.com/Amrit-Code/upstreamwatch/internal/report"
)

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var (
	_ report.Notifier = (*email.Notifier)(nil)
	_ checker         = (*email.Notifier)(nil)
)

const (
	testUser     = "upw-sender"
	testPassword = "example-password-7f3a"
)

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// testDigest has non-ASCII text, a long line and a line starting with a dot,
// so quoted-printable encoding and dot-stuffing are both exercised.
func testDigest() model.Digest {
	return model.Digest{
		GeneratedAt: time.Date(2026, 3, 1, 8, 0, 0, 0, time.FixedZone("CET", 3600)),
		New: []model.DigestItem{{
			FindingID: 7,
			Kind:      model.KindAdvisory,
			Score:     81,
			Tag:       "[A]",
			Headline:  "high  GO-2026-0001  pkg:golang/example.com/lib 1.2.0  (repo: example-server)",
			Lines: []string{
				"Example advisory — a summary long enough to need a soft line break in quoted-printable output, which wraps at 76.",
				".a line that starts with a dot",
			},
			URL: "https://upw.example.org/f/7",
		}},
		Open:        1,
		FindingsURL: "https://upw.example.org/",
	}
}

func newNotifier(t *testing.T, opts email.Options, logger *slog.Logger) *email.Notifier {
	t.Helper()
	n, err := email.New(opts, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return n
}

// dialTo returns a dial function that connects to addr whatever address it
// is asked for.
func dialTo(addr string) func(ctx context.Context, network, _ string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
}

func TestSend_Plain(t *testing.T) {
	t.Parallel()
	srv := newFakeSMTP(t, fakeConfig{})
	n := newNotifier(t, email.Options{
		Host: "127.0.0.1",
		Port: srv.port(),
		From: "upstreamwatch <upw@example.org>",
		To:   []string{"ops@example.org", "Dev Team <dev@example.org>"},
	}, discardLogger())
	d := testDigest()

	if err := n.Send(t.Context(), d); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got := srv.result()

	if want := []string{"EHLO", "MAIL", "RCPT", "RCPT", "DATA", "QUIT"}; !reflect.DeepEqual(got.commands, want) {
		t.Errorf("commands = %v, want %v", got.commands, want)
	}
	if want := "FROM:<upw@example.org>"; got.mailFrom != want {
		t.Errorf("MAIL arg = %q, want %q", got.mailFrom, want)
	}
	if want := []string{"TO:<ops@example.org>", "TO:<dev@example.org>"}; !reflect.DeepEqual(got.rcpts, want) {
		t.Errorf("RCPT args = %v, want %v", got.rcpts, want)
	}
	if bareLF(got.data) {
		t.Errorf("message has a bare LF line ending:\n%q", got.data)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(got.data))
	if err != nil {
		t.Fatalf("parse received message: %v", err)
	}
	h := msg.Header
	wantHeaders := map[string]string{
		"From":                      `"upstreamwatch" <upw@example.org>`,
		"To":                        `<ops@example.org>, "Dev Team" <dev@example.org>`,
		"Mime-Version":              "1.0",
		"Content-Type":              "text/plain; charset=utf-8",
		"Content-Transfer-Encoding": "quoted-printable",
		"Auto-Submitted":            "auto-generated",
		"Date":                      "Sun, 01 Mar 2026 08:00:00 +0100",
	}
	for k, want := range wantHeaders {
		if g := h.Get(k); g != want {
			t.Errorf("header %s = %q, want %q", k, g, want)
		}
	}
	date, err := h.Date()
	if err != nil || !date.Equal(d.GeneratedAt) {
		t.Errorf("Date header parses to %v, %v; want %v", date, err, d.GeneratedAt)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(h.Get("Subject"))
	if err != nil || subject != d.Subject() {
		t.Errorf("decoded Subject = %q, %v; want %q", subject, err, d.Subject())
	}
	if !strings.HasPrefix(h.Get("Subject"), "=?utf-8?q?") {
		t.Errorf("Subject = %q, want it Q-encoded", h.Get("Subject"))
	}
	if id := h.Get("Message-Id"); !regexp.MustCompile(`^<[0-9a-f]{32}@example\.org>$`).MatchString(id) {
		t.Errorf("Message-ID = %q, want <32 hex>@example.org", id)
	}

	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if g, want := strings.ReplaceAll(string(body), "\r\n", "\n"), d.PlainText(); g != want {
		t.Errorf("decoded body = %q, want %q", g, want)
	}
}

func TestSend_STARTTLSAuth(t *testing.T) {
	t.Parallel()
	serverTLS, pool := selfSignedTLS(t)
	srv := newFakeSMTP(t, fakeConfig{startTLS: serverTLS, auth: true})
	var logs bytes.Buffer
	n := newNotifier(t, email.Options{
		Host:     "127.0.0.1",
		Port:     srv.port(),
		Username: testUser,
		Password: testPassword,
		From:     "upw@example.org",
		To:       []string{"ops@example.org"},
		RootCAs:  pool,
	}, slog.New(slog.NewTextHandler(&logs, nil)))

	if err := n.Send(t.Context(), testDigest()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got := srv.result()

	if want := []string{"EHLO", "STARTTLS", "EHLO", "AUTH", "MAIL", "RCPT", "DATA", "QUIT"}; !reflect.DeepEqual(got.commands, want) {
		t.Errorf("commands = %v, want %v", got.commands, want)
	}
	if !got.tlsAtAuth {
		t.Error("AUTH was sent before the connection was upgraded to TLS")
	}
	if want := "\x00" + testUser + "\x00" + testPassword; got.authPayload != want {
		t.Errorf("AUTH PLAIN payload = %q, want %q", got.authPayload, want)
	}

	line := logs.String()
	if strings.Count(line, "\n") != 1 {
		t.Errorf("Send logged %d lines, want 1:\n%s", strings.Count(line, "\n"), line)
	}
	for _, want := range []string{"level=INFO", "recipients=1", "host=127.0.0.1"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q does not contain %q", line, want)
		}
	}
	for _, leak := range []string{testPassword, "GO-2026-0001"} {
		if strings.Contains(line, leak) {
			t.Errorf("log line %q contains %q", line, leak)
		}
	}
}

func TestSend_ImplicitTLS(t *testing.T) {
	t.Parallel()
	serverTLS, pool := selfSignedTLS(t)
	srv := newFakeSMTP(t, fakeConfig{implicitTLS: serverTLS, auth: true})
	n := newNotifier(t, email.Options{
		Host:     "localhost",
		Port:     465,
		Username: testUser,
		Password: testPassword,
		From:     "upw@example.org",
		To:       []string{"ops@example.org"},
		RootCAs:  pool,
	}, discardLogger())
	email.SetDial(n, dialTo(srv.addr()))

	if err := n.Send(t.Context(), testDigest()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got := srv.result()
	if want := []string{"EHLO", "AUTH", "MAIL", "RCPT", "DATA", "QUIT"}; !reflect.DeepEqual(got.commands, want) {
		t.Errorf("commands = %v, want %v", got.commands, want)
	}
	if !got.tlsAtAuth {
		t.Error("AUTH arrived on an unencrypted connection")
	}
}

func TestSend_NoTLSRefusedForRemoteHost(t *testing.T) {
	t.Parallel()
	srv := newFakeSMTP(t, fakeConfig{auth: true})
	n := newNotifier(t, email.Options{
		Host:     "smtp.invalid-example.test",
		Port:     587,
		Username: testUser,
		Password: testPassword,
		From:     "upw@example.org",
		To:       []string{"ops@example.org"},
	}, discardLogger())
	email.SetDial(n, dialTo(srv.addr()))

	err := n.Send(t.Context(), testDigest())
	if err == nil {
		t.Fatal("Send() error = nil, want an error for a server without STARTTLS")
	}
	if want := "email: smtp.invalid-example.test:587: starttls: "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Send() error = %q, want prefix %q", err, want)
	}
	got := srv.result()
	for _, c := range got.commands {
		if c == "AUTH" || c == "MAIL" || c == "DATA" {
			t.Errorf("server received %s; commands = %v", c, got.commands)
		}
	}
	if got.authPayload != "" {
		t.Errorf("server received an AUTH payload %q", got.authPayload)
	}
}

func TestSend_ErrorHasNoPassword(t *testing.T) {
	t.Parallel()
	serverTLS, pool := selfSignedTLS(t)
	srv := newFakeSMTP(t, fakeConfig{startTLS: serverTLS, auth: true, rejectAuth: true})
	n := newNotifier(t, email.Options{
		Host:     "127.0.0.1",
		Port:     srv.port(),
		Username: testUser,
		Password: testPassword,
		From:     "upw@example.org",
		To:       []string{"ops@example.org"},
		RootCAs:  pool,
	}, discardLogger())

	err := n.Send(t.Context(), testDigest())
	if err == nil {
		t.Fatal("Send() error = nil, want the 535 rejection")
	}
	msg := err.Error()
	if !strings.Contains(msg, ": auth: ") || !strings.Contains(msg, "535") {
		t.Errorf("Send() error = %q, want it to name the auth step and the 535 reply", msg)
	}
	// The fake echoes the AUTH payload in its 535 text, so the password
	// reached the error chain and must have been replaced.
	if strings.Contains(msg, testPassword) {
		t.Errorf("Send() error = %q contains the password", msg)
	}
	if !strings.Contains(msg, "[REDACTED]") {
		t.Errorf("Send() error = %q, want the echoed password replaced by [REDACTED]", msg)
	}
	for _, c := range srv.result().commands {
		if c == "MAIL" {
			t.Error("server received MAIL after a rejected AUTH")
		}
	}
}

func TestSend_ServerUntrustedCertificate(t *testing.T) {
	t.Parallel()
	serverTLS, _ := selfSignedTLS(t)
	srv := newFakeSMTP(t, fakeConfig{startTLS: serverTLS, auth: true})
	n := newNotifier(t, email.Options{
		Host:     "127.0.0.1",
		Port:     srv.port(),
		Username: testUser,
		Password: testPassword,
		From:     "upw@example.org",
		To:       []string{"ops@example.org"},
		RootCAs:  x509.NewCertPool(),
	}, discardLogger())

	err := n.Send(t.Context(), testDigest())
	if err == nil || !strings.Contains(err.Error(), ": starttls: ") {
		t.Errorf("Send() error = %v, want a starttls failure", err)
	}
	if got := srv.result(); got.authPayload != "" {
		t.Errorf("server received AUTH %q over an unverified connection", got.authPayload)
	}
}

func TestSend_CancelDuringImplicitTLS(t *testing.T) {
	t.Parallel()
	serverTLS, pool := selfSignedTLS(t)
	srv := newFakeSMTP(t, fakeConfig{implicitTLS: serverTLS, hang: true})
	n := newNotifier(t, email.Options{
		Host:    "localhost",
		Port:    465,
		From:    "upw@example.org",
		To:      []string{"ops@example.org"},
		RootCAs: pool,
	}, discardLogger())
	email.SetDial(n, dialTo(srv.addr()))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errc := make(chan error, 1)
	go func() { errc <- n.Send(ctx, testDigest()) }()
	// Cancel once the TLS handshake is done, while the client waits for the
	// greeting on the TLS connection.
	select {
	case <-srv.handshook:
	case err := <-errc:
		t.Fatalf("Send() returned %v before the TLS handshake completed", err)
	}
	cancel()

	err := <-errc
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Send() error = %v, want context.Canceled", err)
	}
}

func TestSend_QUITFailureAfterAcceptedMessage(t *testing.T) {
	t.Parallel()
	srv := newFakeSMTP(t, fakeConfig{dropAtQUIT: true})
	var logs bytes.Buffer
	n := newNotifier(t, email.Options{
		Host: "127.0.0.1",
		Port: srv.port(),
		From: "upw@example.org",
		To:   []string{"ops@example.org"},
	}, slog.New(slog.NewTextHandler(&logs, nil)))

	// The server accepted the message, so a lost QUIT reply must not make
	// the digest count as failed and be sent again.
	if err := n.Send(t.Context(), testDigest()); err != nil {
		t.Fatalf("Send() error = %v, want nil after the message was accepted", err)
	}
	if got := srv.result(); got.data == nil {
		t.Fatal("server received no message")
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, ": quit: ") {
		t.Errorf("logs = %q, want a WARN line naming the quit step", out)
	}
}

func TestCheck_QUITFailure(t *testing.T) {
	t.Parallel()
	srv := newFakeSMTP(t, fakeConfig{dropAtQUIT: true})
	n := newNotifier(t, email.Options{
		Host: "127.0.0.1",
		Port: srv.port(),
		From: "upw@example.org",
		To:   []string{"ops@example.org"},
	}, discardLogger())

	err := n.Check(t.Context())
	if err == nil || !strings.Contains(err.Error(), ": quit: ") {
		t.Errorf("Check() error = %v, want a quit failure", err)
	}
}

func TestSend_TimeoutWhenServerHangs(t *testing.T) {
	t.Parallel()
	srv := newFakeSMTP(t, fakeConfig{hang: true})
	n := newNotifier(t, email.Options{
		Host:    "127.0.0.1",
		Port:    srv.port(),
		From:    "upw@example.org",
		To:      []string{"ops@example.org"},
		Timeout: 100 * time.Millisecond,
	}, discardLogger())

	err := n.Send(t.Context(), testDigest())
	if err == nil {
		t.Fatal("Send() error = nil, want a timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("Send() error = %v, want a deadline error", err)
	}
	if !strings.Contains(err.Error(), ": greeting: ") {
		t.Errorf("Send() error = %q, want it to name the greeting step", err)
	}
}

func TestSend_CancelledContext(t *testing.T) {
	t.Parallel()
	srv := newFakeSMTP(t, fakeConfig{})
	n := newNotifier(t, email.Options{
		Host: "127.0.0.1",
		Port: srv.port(),
		From: "upw@example.org",
		To:   []string{"ops@example.org"},
	}, discardLogger())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := n.Send(ctx, testDigest())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Send() error = %v, want context.Canceled", err)
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	serverTLS, pool := selfSignedTLS(t)
	srv := newFakeSMTP(t, fakeConfig{startTLS: serverTLS, auth: true})
	n := newNotifier(t, email.Options{
		Host:     "127.0.0.1",
		Port:     srv.port(),
		Username: testUser,
		Password: testPassword,
		From:     "upw@example.org",
		To:       []string{"ops@example.org"},
		RootCAs:  pool,
	}, discardLogger())

	if err := n.Check(t.Context()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	got := srv.result()
	if want := []string{"EHLO", "STARTTLS", "EHLO", "AUTH", "QUIT"}; !reflect.DeepEqual(got.commands, want) {
		t.Errorf("commands = %v, want %v", got.commands, want)
	}
	if got.data != nil {
		t.Errorf("Check() sent a message: %q", got.data)
	}
}

func TestCheck_NoAuthWithoutUsername(t *testing.T) {
	t.Parallel()
	srv := newFakeSMTP(t, fakeConfig{auth: true})
	n := newNotifier(t, email.Options{
		Host: "localhost",
		Port: srv.port(),
		From: "upw@example.org",
		To:   []string{"ops@example.org"},
	}, discardLogger())
	email.SetDial(n, dialTo(srv.addr()))

	if err := n.Check(t.Context()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if got, want := srv.result().commands, []string{"EHLO", "QUIT"}; !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestCheck_AuthRejected(t *testing.T) {
	t.Parallel()
	serverTLS, pool := selfSignedTLS(t)
	srv := newFakeSMTP(t, fakeConfig{startTLS: serverTLS, auth: true, rejectAuth: true})
	n := newNotifier(t, email.Options{
		Host:     "127.0.0.1",
		Port:     srv.port(),
		Username: testUser,
		Password: testPassword,
		From:     "upw@example.org",
		To:       []string{"ops@example.org"},
		RootCAs:  pool,
	}, discardLogger())

	err := n.Check(t.Context())
	if err == nil || !strings.Contains(err.Error(), ": auth: ") {
		t.Errorf("Check() error = %v, want an auth failure", err)
	}
	if err != nil && strings.Contains(err.Error(), testPassword) {
		t.Errorf("Check() error = %q contains the password", err)
	}
}

func TestNew_Validation(t *testing.T) {
	t.Parallel()
	valid := func() email.Options {
		return email.Options{
			Host:     "smtp.example.org",
			Port:     587,
			Username: testUser,
			Password: testPassword,
			From:     "upw@example.org",
			To:       []string{"ops@example.org"},
		}
	}
	tests := []struct {
		name   string
		mutate func(o *email.Options)
		want   string // substring of the error; empty means no error
	}{
		{"valid", func(*email.Options) {}, ""},
		{"valid without auth", func(o *email.Options) { o.Username, o.Password = "", "" }, ""},
		{"empty host", func(o *email.Options) { o.Host = "" }, "host is empty"},
		{"host with CR", func(o *email.Options) { o.Host = "smtp.example.org\r" }, "host contains CR or LF"},
		{"port zero", func(o *email.Options) { o.Port = 0 }, "port 0 is outside 1..65535"},
		{"port too large", func(o *email.Options) { o.Port = 65536 }, "port 65536 is outside 1..65535"},
		{"username with LF", func(o *email.Options) { o.Username = "a\nb" }, "username contains CR or LF"},
		{"username without password", func(o *email.Options) { o.Password = "" }, "username is set but password is empty"},
		{"password without username", func(o *email.Options) { o.Username = "" }, "password is set but username is empty"},
		{"empty from", func(o *email.Options) { o.From = "" }, "from is empty"},
		{"unparseable from", func(o *email.Options) { o.From = "not an address" }, "from: "},
		{"from with CRLF", func(o *email.Options) { o.From = "upw@example.org\r\nBcc: x@example.org" }, "from contains CR or LF"},
		{"empty to", func(o *email.Options) { o.To = nil }, "to is empty"},
		{"unparseable to", func(o *email.Options) { o.To = []string{"ops@example.org", "nope"} }, "to[1]: "},
		{"to with LF", func(o *email.Options) { o.To = []string{"ops@example.org\nBcc: x@example.org"} }, "to[0] contains CR or LF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := valid()
			tt.mutate(&o)
			n, err := email.New(o, discardLogger())
			if tt.want == "" {
				if err != nil || n == nil {
					t.Fatalf("New() = %v, %v; want a notifier", n, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("New() error = nil, want one containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("New() error = %q, want it to contain %q", err, tt.want)
			}
			if strings.Contains(err.Error(), testPassword) {
				t.Errorf("New() error = %q contains the password", err)
			}
		})
	}
}

func TestNew_ReportsEveryProblem(t *testing.T) {
	t.Parallel()
	_, err := email.New(email.Options{Port: -1}, discardLogger())
	if err == nil {
		t.Fatal("New() error = nil, want errors")
	}
	for _, want := range []string{"host is empty", "port -1", "from is empty", "to is empty"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("New() error = %q, want it to contain %q", err, want)
		}
	}
}

// bareLF reports whether b has an LF not preceded by CR.
func bareLF(b []byte) bool {
	for i, c := range b {
		if c == '\n' && (i == 0 || b[i-1] != '\r') {
			return true
		}
	}
	return false
}
