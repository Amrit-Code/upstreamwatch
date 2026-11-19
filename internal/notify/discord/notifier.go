package discord

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
	"time"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	// maxEmbeds is the most embeds one message carries; Discord accepts ten.
	maxEmbeds = 10
	// maxTitleRunes and maxDescriptionRunes cap one embed's title and
	// description.
	maxTitleRunes       = 256
	maxDescriptionRunes = 1000
	// maxEmbedTotal is the bound the embed characters of one message stay
	// under; Discord rejects a message whose embeds total more than 6,000.
	maxEmbedTotal = 5500
	// checkTimeout bounds the reachability probe.
	checkTimeout = 10 * time.Second
	ellipsis     = "…"
)

// Notifier posts the digest to one Discord incoming webhook through the
// shared HTTP client. The webhook URL is the credential, so it appears in
// errors and logs only as its host.
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
		return "", errors.New("discord: webhook URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return "", errors.New("discord: webhook URL must be an absolute https URL")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return "", fmt.Errorf("discord: webhook URL for %s must use https", u.Host)
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
	var msg any = buildMessage(d)
	if d.Heartbeat {
		msg = buildHeartbeat(d)
	}
	body, err := encodeJSON(msg)
	if err != nil {
		return fmt.Errorf("discord: encode message: %w", err)
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
	n.logger.Info("digest sent", "channel", "discord", "host", n.host, "items", len(d.New)+len(d.Changed))
	return nil
}

// Check fetches the webhook with GET, which Discord answers with the
// webhook's metadata without posting anything.
func (n *Notifier) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	if _, err := n.hc.Do(ctx, httpx.Request{URL: n.url.Reveal(), SecretPath: true}); err != nil {
		return requestError("check", n.host, err)
	}
	n.logger.Debug("discord check passed", "host", n.host)
	return nil
}

// message is the webhook payload of a digest with items.
type message struct {
	Content         string          `json:"content"`
	Embeds          []embed         `json:"embeds"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

// heartbeat is the webhook payload of the "all quiet" message.
type heartbeat struct {
	Content string `json:"content"`
}

type embed struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// allowedMentions with an empty Parse list stops every mention from
// pinging, whatever the text says.
type allowedMentions struct {
	Parse []string `json:"parse"`
}

// buildMessage lays out a digest with items: the subject as content, one
// embed per item for the first ten of New then Changed while the embed text
// stays under maxEmbedTotal, and an "… and N more" line for the rest.
func buildMessage(d model.Digest) message {
	items := make([]model.DigestItem, 0, len(d.New)+len(d.Changed))
	items = append(items, d.New...)
	items = append(items, d.Changed...)

	embeds := make([]embed, 0, maxEmbeds)
	total := 0
	for _, it := range items {
		if len(embeds) == maxEmbeds {
			break
		}
		e := itemEmbed(it)
		size := utf8.RuneCountInString(e.Title) + utf8.RuneCountInString(e.Description)
		if total+size >= maxEmbedTotal {
			break
		}
		total += size
		embeds = append(embeds, e)
	}

	content := d.Subject()
	if more := len(items) - len(embeds) + d.MoreNew + d.MoreChanged; more > 0 {
		content += "\n" + ellipsis + " and " + strconv.Itoa(more) + " more"
		if d.FindingsURL != "" {
			content += " " + d.FindingsURL
		}
	}
	return message{
		Content:         content,
		Embeds:          embeds,
		AllowedMentions: allowedMentions{Parse: []string{}},
	}
}

// itemEmbed lays out one item. Headline and Lines may hold upstream text, so
// their markdown is neutralised.
func itemEmbed(it model.DigestItem) embed {
	prefix := it.Tag + " "
	return embed{
		Title:       prefix + neutralize(it.Headline, maxTitleRunes-utf8.RuneCountInString(prefix)),
		Description: neutralize(strings.Join(it.Lines, "\n"), maxDescriptionRunes),
		URL:         it.URL,
	}
}

// buildHeartbeat lays out the "all quiet" message: the subject and the last
// two lines of the plain-text digest (the last runs and the counts).
func buildHeartbeat(d model.Digest) heartbeat {
	lines := strings.Split(strings.TrimRight(d.PlainText(), "\n"), "\n")
	tail := lines[max(0, len(lines)-2):]
	return heartbeat{Content: d.Subject() + "\n" + strings.Join(tail, "\n")}
}

// neutralize escapes Discord markdown in s and caps the result at limit
// runes, ending a cut text with "…". An escape is never split by the cut.
func neutralize(s string, limit int) string {
	tokens := make([]string, 0, len(s))
	size := 0
	for _, r := range s {
		t := escapeRune(r)
		tokens = append(tokens, t)
		size += utf8.RuneCountInString(t)
	}
	if size <= limit {
		return strings.Join(tokens, "")
	}
	var b strings.Builder
	n := 0
	for _, t := range tokens {
		tl := utf8.RuneCountInString(t)
		if n+tl > limit-1 {
			break
		}
		b.WriteString(t)
		n += tl
	}
	b.WriteString(ellipsis)
	return b.String()
}

// escapeRune returns r with a backslash before each markdown character, and
// "@" followed by a zero-width space so no mention can form.
func escapeRune(r rune) string {
	switch r {
	case '\\', '*', '_', '~', '`', '|', '>', '[', ']':
		return `\` + string(r)
	case '@':
		return "@​"
	}
	return string(r)
}

// encodeJSON marshals v without HTML escaping, which Discord does not need.
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
// reads "discord: post to <host>: status <n>". The text passes through
// model.Redact; the original error stays reachable through Unwrap.
func requestError(op, host string, err error) error {
	msg := fmt.Sprintf("discord: %s %s: %v", op, host, err)
	if se, ok := errors.AsType[*httpx.StatusError](err); ok {
		msg = fmt.Sprintf("discord: %s %s: status %d", op, host, se.Status)
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
