package email_test

import (
	"bytes"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/email"
)

// qpLines returns the body lines the notifier writes for d: its plain text
// quoted-printable encoded, as net/smtp then sends them after DATA.
func qpLines(t *testing.T, d model.Digest) []string {
	t.Helper()
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	if _, err := w.Write([]byte(d.PlainText())); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return strings.Split(b.String(), "\r\n")
}

// dotDigest returns a digest whose encoded body has a line for which match
// is true, found by padding the headline, so the test does not depend on
// where the encoder wraps.
func dotDigest(t *testing.T, tail string, match func(line string) bool) model.Digest {
	t.Helper()
	for pad := 40; pad < 160; pad++ {
		d := model.Digest{
			GeneratedAt: time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC),
			New: []model.DigestItem{{
				FindingID: 1, Kind: model.KindAdvisory, Tag: "[A]",
				Headline: strings.Repeat("a", pad) + tail,
				Lines:    []string{"summary"},
			}},
			Open: 1,
		}
		for _, l := range qpLines(t, d) {
			if match(l) {
				return d
			}
		}
	}
	t.Fatalf("no padding puts %q at the start of an encoded line", tail)
	return model.Digest{}
}

// A body line that starts with "." must be dot-stuffed on the wire, and a
// line that is only "." must not end the message early: either way the
// server must receive the body exactly.
func TestSend_BodyLineStartingWithDotSurvives(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		tail  string
		match func(string) bool
	}{
		{"line starting with a dot", ".hidden text after the wrap",
			func(l string) bool { return strings.HasPrefix(l, ".hidden") }},
		{"line that is a lone dot", ".",
			func(l string) bool { return l == "." }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := dotDigest(t, tt.tail, tt.match)
			srv := newFakeSMTP(t, fakeConfig{})
			n := newNotifier(t, email.Options{
				Host: "127.0.0.1",
				Port: srv.port(),
				From: "upw@example.org",
				To:   []string{"ops@example.org"},
			}, discardLogger())
			if err := n.Send(t.Context(), d); err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			got := srv.result()
			if want := []string{"EHLO", "MAIL", "RCPT", "DATA", "QUIT"}; strings.Join(got.commands, ",") != strings.Join(want, ",") {
				t.Errorf("commands = %v, want %v (body lines must not reach the server as commands)", got.commands, want)
			}
			msg, err := mail.ReadMessage(bytes.NewReader(got.data))
			if err != nil {
				t.Fatalf("parse received message: %v", err)
			}
			body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
			if err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if g, want := strings.ReplaceAll(string(body), "\r\n", "\n"), d.PlainText(); g != want {
				t.Errorf("decoded body = %q, want %q", g, want)
			}
		})
	}
}

// Configuration that would put a line break into a header is refused before
// any connection is made.
func TestNew_RefusesHeaderInjection(t *testing.T) {
	t.Parallel()
	valid := email.Options{Host: "smtp.example.org", Port: 587, From: "upw@example.org", To: []string{"ops@example.org"}}
	tests := []struct {
		name   string
		mutate func(o *email.Options)
	}{
		{"from display name with CRLF", func(o *email.Options) { o.From = "\"Ops\r\nBcc: x@example.org\" <upw@example.org>" }},
		{"from with a bare CR", func(o *email.Options) { o.From = "upw@example.org\rBcc: x@example.org" }},
		{"to display name with LF", func(o *email.Options) { o.To = []string{"\"Ops\nBcc: x@example.org\" <ops@example.org>"} }},
		{"second to with CRLF", func(o *email.Options) { o.To = []string{"ops@example.org", "dev@example.org\r\nBcc: x@example.org"} }},
		{"host with LF", func(o *email.Options) { o.Host = "smtp.example.org\nX-Injected: 1" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := valid
			o.To = append([]string(nil), valid.To...)
			tt.mutate(&o)
			n, err := email.New(o, discardLogger())
			if err == nil {
				t.Fatalf("New() = %v, nil; want an error", n)
			}
			if !strings.Contains(err.Error(), "CR or LF") {
				t.Errorf("New() error = %q, want it to name the line break", err)
			}
		})
	}
}
