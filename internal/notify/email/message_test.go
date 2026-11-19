package email

import (
	"bytes"
	"errors"
	"mime"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestSubjectEncoding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    string // exact encoded value; empty when only round-trip is checked
		encoded bool
		wantErr error
	}{
		{name: "ascii unchanged", in: "upstreamwatch - 2026-03-01 - all quiet", want: "upstreamwatch - 2026-03-01 - all quiet"},
		{name: "em dash Q-encoded", in: "upstreamwatch — 2026-03-01 — 2 new, 0 changed", encoded: true},
		{name: "CRLF rejected", in: "all quiet\r\nBcc: x@example.org", wantErr: errHeaderBreak},
		{name: "LF rejected", in: "all quiet\nBcc: x@example.org", wantErr: errHeaderBreak},
		{name: "CR rejected", in: "all quiet\r", wantErr: errHeaderBreak},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := encodeSubject(tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("encodeSubject(%q) error = %v, want %v", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("encodeSubject(%q) error = %v", tt.in, err)
			}
			if tt.want != "" && got != tt.want {
				t.Errorf("encodeSubject(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if tt.encoded && !strings.HasPrefix(got, "=?utf-8?q?") {
				t.Errorf("encodeSubject(%q) = %q, want a Q-encoded word", tt.in, got)
			}
			for _, b := range []byte(got) {
				if b >= 0x80 {
					t.Fatalf("encodeSubject(%q) = %q has non-ASCII bytes", tt.in, got)
				}
			}
			decoded, err := new(mime.WordDecoder).DecodeHeader(got)
			if err != nil || decoded != tt.in {
				t.Errorf("decode(encodeSubject(%q)) = %q, %v; want the input", tt.in, decoded, err)
			}
		})
	}
}

func TestBuildMessage_CRLFAndHeaderOrder(t *testing.T) {
	t.Parallel()
	from, _ := mail.ParseAddress("upw@example.org")
	to, _ := mail.ParseAddress("ops@example.org")
	d := model.Digest{
		GeneratedAt: time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC),
		Heartbeat:   true,
	}

	msg, err := buildMessage(d, envelope{from: from, to: []*mail.Address{to}})
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	if bytes.Count(msg, []byte("\n")) != bytes.Count(msg, []byte("\r\n")) {
		t.Errorf("buildMessage() has a bare LF:\n%q", msg)
	}
	head, _, ok := bytes.Cut(msg, []byte("\r\n\r\n"))
	if !ok {
		t.Fatalf("buildMessage() has no blank line after the headers:\n%q", msg)
	}
	var names []string
	for line := range strings.SplitSeq(string(head), "\r\n") {
		name, _, _ := strings.Cut(line, ":")
		names = append(names, name)
	}
	want := []string{"From", "To", "Subject", "Date", "Message-ID", "MIME-Version", "Content-Type", "Content-Transfer-Encoding", "Auto-Submitted"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("header names = %v, want %v", names, want)
	}
}

func TestMessageID(t *testing.T) {
	t.Parallel()
	a, err := messageID("upw@mail.example.org")
	if err != nil {
		t.Fatalf("messageID() error = %v", err)
	}
	b, err := messageID("upw@mail.example.org")
	if err != nil {
		t.Fatalf("messageID() error = %v", err)
	}
	if !strings.HasSuffix(a, "@mail.example.org>") || !strings.HasPrefix(a, "<") || len(a) != len("<@mail.example.org>")+32 {
		t.Errorf("messageID() = %q, want <32 hex>@mail.example.org", a)
	}
	if a == b {
		t.Errorf("messageID() returned %q twice", a)
	}
}
