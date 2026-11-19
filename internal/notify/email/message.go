package email

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// errHeaderBreak is returned when a rendered header value contains CR or LF,
// which would let it start a new header.
var errHeaderBreak = errors.New("contains CR or LF")

// envelope is the parsed sender and recipients a message is built for.
type envelope struct {
	from *mail.Address
	to   []*mail.Address
}

// buildMessage renders d as a plain-text, quoted-printable email with CRLF
// line endings, ready to be written after DATA.
func buildMessage(d model.Digest, env envelope) ([]byte, error) {
	subject, err := encodeSubject(d.Subject())
	if err != nil {
		return nil, fmt.Errorf("subject: %w", err)
	}
	id, err := messageID(env.from.Address)
	if err != nil {
		return nil, fmt.Errorf("message-id: %w", err)
	}
	to := make([]string, len(env.to))
	for i, a := range env.to {
		to[i] = a.String()
	}

	var b bytes.Buffer
	header := func(name, value string) {
		b.WriteString(name + ": " + value + "\r\n")
	}
	header("From", env.from.String())
	header("To", strings.Join(to, ", "))
	header("Subject", subject)
	header("Date", d.GeneratedAt.Format(time.RFC1123Z))
	header("Message-ID", id)
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	header("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")

	// The quoted-printable writer turns every LF in the text into CRLF.
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(d.PlainText())); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	if err := qp.Close(); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	return b.Bytes(), nil
}

// encodeSubject returns s as a Subject header value: Q-encoded UTF-8 when it
// has non-ASCII bytes, unchanged otherwise. A subject with CR or LF is an
// error.
func encodeSubject(s string) (string, error) {
	if strings.ContainsAny(s, "\r\n") {
		return "", errHeaderBreak
	}
	return mime.QEncoding.Encode("utf-8", s), nil
}

// messageID returns a new Message-ID value, <random hex>@<domain of addr>.
func messageID(addr string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	domain := addr[strings.LastIndexByte(addr, '@')+1:]
	return "<" + hex.EncodeToString(buf) + "@" + domain + ">", nil
}
