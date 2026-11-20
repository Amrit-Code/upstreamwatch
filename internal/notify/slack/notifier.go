package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	// maxSections is the most items laid out as section blocks. With the
	// header and context blocks a message has at most 22 blocks, under
	// Slack's limit of 50.
	maxSections = 20
	// maxSectionRunes caps one section's text, under the 3,000 characters
	// Slack accepts.
	maxSectionRunes = 2900
	ellipsis        = "…"
)

// Notifier posts the digest to one Slack incoming webhook through the shared
// HTTP client. The webhook URL is the credential, so it appears in errors
// and logs only as its host.
type Notifier struct {
	hc     *httpx.Client
	url    model.Secret
	host   string
	logger *slog.Logger
}

// New returns a Notifier for the webhook at url. The URL must be absolute
// and use https, or http for a loopback host. It does no I/O.
func New(hc *httpx.Client, url model.Secret, logger *slog.Logger) (*Notifier, error) {
	host, err := validateURL(url.Reveal())
	if err != nil {
		return nil, err
	}
	return &Notifier{hc: hc, url: url, host: host, logger: logger}, nil
}

// validateURL checks the webhook URL and returns its host. Errors name at
// most the host, never the path, which holds the webhook token.
func validateURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("slack: webhook URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return "", errors.New("slack: webhook URL must be an absolute https URL")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return "", fmt.Errorf("slack: webhook URL for %s must use https", u.Host)
	}
	return u.Host, nil
}

// isLoopback reports whether host is localhost or a loopback address.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

// Send posts d to the webhook. An answer other than 2xx is an error.
func (n *Notifier) Send(ctx context.Context, d model.Digest) error {
	body, err := encodeJSON(buildMessage(d))
	if err != nil {
		return fmt.Errorf("slack: encode message: %w", err)
	}
	_, err = n.hc.Do(ctx, httpx.Request{
		Method:     http.MethodPost,
		URL:        n.url.Reveal(),
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       body,
		SecretPath: true,
	})
	if err != nil {
		return requestError("post to", n.host, err)
	}
	n.logger.Info("digest sent", "channel", "slack", "host", n.host, "items", len(d.New)+len(d.Changed))
	return nil
}

// Check validates the webhook URL and returns nil without a request. Slack
// incoming webhooks have no read-only probe: any request that reaches one
// either posts a message or fails as a malformed post, so reachability
// cannot be proved without sending something.
func (n *Notifier) Check(context.Context) error {
	_, err := validateURL(n.url.Reveal())
	return err
}

// message is the webhook payload. Text is the notification fallback.
type message struct {
	Text   string  `json:"text"`
	Blocks []block `json:"blocks"`
}

// block is one Block Kit block: a header or section carries Text, a context
// block carries Elements.
type block struct {
	Type     string `json:"type"`
	Text     *text  `json:"text,omitempty"`
	Elements []text `json:"elements,omitempty"`
}

type text struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// buildMessage lays out d: a header block with the subject, a section per
// item for the first twenty of New then Changed, and a context block with
// the counts line and an "… and N more" line for the items left out.
func buildMessage(d model.Digest) message {
	items := make([]model.DigestItem, 0, len(d.New)+len(d.Changed))
	items = append(items, d.New...)
	items = append(items, d.Changed...)
	shown := items[:min(len(items), maxSections)]

	subject := d.Subject()
	blocks := make([]block, 0, len(shown)+2)
	blocks = append(blocks, block{Type: "header", Text: &text{Type: "plain_text", Text: subject}})
	for _, it := range shown {
		blocks = append(blocks, block{Type: "section", Text: &text{Type: "mrkdwn", Text: sectionText(it)}})
	}

	counts := "Open: " + strconv.Itoa(d.Open) + ", acked: " + strconv.Itoa(d.Acked) + ", snoozed: " + strconv.Itoa(d.Snoozed) + "."
	elements := []text{{Type: "mrkdwn", Text: counts}}
	if more := len(items) - len(shown) + d.MoreNew + d.MoreChanged; more > 0 {
		line := ellipsis + " and " + strconv.Itoa(more) + " more"
		if d.FindingsURL != "" {
			line += " " + d.FindingsURL
		}
		elements = append(elements, text{Type: "mrkdwn", Text: line})
	}
	blocks = append(blocks, block{Type: "context", Elements: elements})
	return message{Text: subject, Blocks: blocks}
}

// sectionText lays out one item as mrkdwn: the bold tag and headline, the
// detail lines, and a link to the finding when it has a URL. Tag, Headline
// and Lines are escaped, which stops upstream text from forming links or
// mentions. The text is capped at maxSectionRunes: the detail lines are cut
// first so the headline and link stay whole, and only a headline too long
// on its own cuts the whole text.
func sectionText(it model.DigestItem) string {
	head := "*" + escape(it.Tag+" "+it.Headline) + "*"
	var lines, link string
	if len(it.Lines) > 0 {
		lines = "\n" + escape(strings.Join(it.Lines, "\n"))
	}
	if it.URL != "" {
		link = "\n<" + it.URL + "|Open>"
	}
	room := maxSectionRunes - utf8.RuneCountInString(head) - utf8.RuneCountInString(link)
	if room < 0 {
		return capRunes(head+lines+link, maxSectionRunes)
	}
	return head + capRunes(lines, room) + link
}

// capRunes returns s cut to at most limit runes, ending a cut text with
// "…". A limit below one rune leaves nothing.
func capRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	if limit < 1 {
		return ""
	}
	n := 0
	for i := range s {
		if n == limit-1 {
			return s[:i] + ellipsis
		}
		n++
	}
	return s
}

// zeroWidthSpace goes before each mrkdwn formatting character so that Slack
// shows the character as it is instead of reading it as a marker.
const zeroWidthSpace = "\u200b"

// escaper replaces the three characters Slack treats as control characters
// in message text, and puts a zero-width space before each mrkdwn
// formatting character (*, _, ~ and `), so upstream text can neither form
// markup nor close the bold wrapper around a headline.
var escaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;",
	"*", zeroWidthSpace+"*", "_", zeroWidthSpace+"_", "~", zeroWidthSpace+"~", "`", zeroWidthSpace+"`",
)

// escape returns s with Slack's control characters and mrkdwn formatting
// characters escaped.
func escape(s string) string { return escaper.Replace(s) }

// encodeJSON marshals v without HTML escaping, which Slack does not need.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// requestError describes a failed request by the host alone. A status error
// reads "slack: post to <host>: status <n>". The text passes through
// model.Redact; the original error stays reachable through Unwrap.
func requestError(op, host string, err error) error {
	msg := fmt.Sprintf("slack: %s %s: %v", op, host, err)
	if se, ok := errors.AsType[*httpx.StatusError](err); ok {
		msg = fmt.Sprintf("slack: %s %s: status %d", op, host, se.Status)
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
