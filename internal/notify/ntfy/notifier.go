package ntfy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	// maxBodyBytes is the most bytes of message body sent, "…" included;
	// ntfy turns a longer message into an attachment.
	maxBodyBytes = 4000
	// checkTimeout bounds the health probe.
	checkTimeout = 10 * time.Second
	ellipsis     = "…"
)

// Notifier publishes the digest to one ntfy topic through the shared HTTP
// client. The topic URL is the credential, so it appears in errors and logs
// only as its host.
type Notifier struct {
	hc        *httpx.Client
	url       model.Secret
	host      string
	healthURL string
	logger    *slog.Logger
}

// New returns a Notifier for the topic at url. The URL must be absolute and
// use https, or http for a loopback host. It does no I/O.
func New(hc *httpx.Client, url model.Secret, logger *slog.Logger) (*Notifier, error) {
	u, err := parseURL(url.Reveal())
	if err != nil {
		return nil, err
	}
	return &Notifier{hc: hc, url: url, host: u.Host, healthURL: healthURLOf(u), logger: logger}, nil
}

// healthURLOf returns <scheme>://<host>/v1/health for the topic URL u.
func healthURLOf(u *url.URL) string {
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/v1/health"}).String()
}

// parseURL checks the topic URL. Errors name at most the host, never the
// path, which names the topic.
func parseURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("ntfy: topic URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil, errors.New("ntfy: topic URL must be an absolute https URL")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return nil, fmt.Errorf("ntfy: topic URL for %s must use https", u.Host)
	}
	return u, nil
}

// isLoopback reports whether host is localhost or a loopback address.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

// Send publishes the plain-text digest to the topic, with the subject as
// title and the findings page as click target. An answer other than 2xx is
// an error.
func (n *Notifier) Send(ctx context.Context, d model.Digest) error {
	h := http.Header{
		"Content-Type": {"text/plain; charset=utf-8"},
		"Title":        {asciiTitle(d.Subject())},
		"Priority":     {"default"},
	}
	if d.FindingsURL != "" {
		h.Set("Click", d.FindingsURL)
	}
	switch {
	case d.Heartbeat:
		h.Set("Tags", "white_check_mark")
	case !d.IsEmpty():
		h.Set("Tags", "warning")
	}
	_, err := n.hc.Do(ctx, httpx.Request{
		Method:     http.MethodPost,
		URL:        n.url.Reveal(),
		Header:     h,
		Body:       []byte(truncate(d.PlainText())),
		SecretPath: true,
	})
	if err != nil {
		return requestError("post to", n.host, err)
	}
	n.logger.Info("digest sent", "channel", "ntfy", "host", n.host, "items", len(d.New)+len(d.Changed))
	return nil
}

// health is the answer of ntfy's /v1/health endpoint.
type health struct {
	Healthy bool `json:"healthy"`
}

// Check asks the server's /v1/health endpoint whether it is healthy. It
// publishes nothing.
func (n *Notifier) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	resp, err := n.hc.Do(ctx, httpx.Request{URL: n.healthURL})
	if err != nil {
		return requestError("check", n.host, err)
	}
	var h health
	if err := json.Unmarshal(resp.Body, &h); err != nil {
		return fmt.Errorf("ntfy: check %s: decode health answer: %w", n.host, err)
	}
	if !h.Healthy {
		return fmt.Errorf("ntfy: check %s: server reports unhealthy", n.host)
	}
	n.logger.Debug("ntfy check passed", "host", n.host)
	return nil
}

// truncate caps s at maxBodyBytes, cutting on a UTF-8 boundary and ending
// a cut text with "…".
func truncate(s string) string {
	if len(s) <= maxBodyBytes {
		return s
	}
	cut := maxBodyBytes - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}

// asciiTitle returns s with "—" replaced by "-" and every other non-ASCII
// character dropped, because ntfy reads header values as ASCII.
func asciiTitle(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '—':
			b.WriteByte('-')
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// requestError describes a failed request by the host alone. A status error
// reads "ntfy: post to <host>: status <n>". The text passes through
// model.Redact; the original error stays reachable through Unwrap.
func requestError(op, host string, err error) error {
	msg := fmt.Sprintf("ntfy: %s %s: %v", op, host, err)
	if se, ok := errors.AsType[*httpx.StatusError](err); ok {
		msg = fmt.Sprintf("ntfy: %s %s: status %d", op, host, se.Status)
	}
	return &redactedError{msg: model.Redact(msg), err: err}
}

// redactedError is an error whose text has passed through model.Redact.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }
