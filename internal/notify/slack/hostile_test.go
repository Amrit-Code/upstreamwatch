package slack_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// hostileTexts are upstream strings that try to ping people or to form a
// link in a Slack message.
var hostileTexts = []string{
	"@everyone",
	"@here",
	"<@U012ABCDEF>",
	"<!channel>",
	"<!here|here>",
	"<!subteam^S012ABC>",
	"<#C012ABCDEF>",
	"[click](https://evil.example)",
	"<https://evil.example|x>",
	"<https://evil.example>",
	"&lt;https://evil.example|x&gt;",
	"a > b < c & d",
}

// unescapeSlack reverses Slack's three escapes and fails the test when s
// holds a '<' or '>' (which would open or close a link or mention) or an
// '&' that does not start one of the escapes.
func unescapeSlack(t *testing.T, field, s string) string {
	t.Helper()
	if strings.ContainsAny(s, "<>") {
		t.Errorf("%s %q: holds '<' or '>', so a link or mention can form", field, s)
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		switch {
		case strings.HasPrefix(s[i:], "&amp;"):
			b.WriteByte('&')
			i += len("&amp;") - 1
		case strings.HasPrefix(s[i:], "&lt;"):
			b.WriteByte('<')
			i += len("&lt;") - 1
		case strings.HasPrefix(s[i:], "&gt;"):
			b.WriteByte('>')
			i += len("&gt;") - 1
		default:
			t.Errorf("%s %q: bare '&' at byte %d", field, s, i)
			b.WriteByte('&')
		}
	}
	return b.String()
}

func TestSlackSend_HostileTextCannotMentionOrLink(t *testing.T) {
	t.Parallel()
	const findingURL = "https://upw.example.com/f/1"
	for _, hostile := range hostileTexts {
		t.Run(hostile, func(t *testing.T) {
			t.Parallel()
			w := newWebhook(t, http.StatusOK)
			n := newNotifier(t, w, io.Discard)
			headline := "high  GHSA-xxxx  " + hostile
			line := "summary " + hostile
			d := model.Digest{
				GeneratedAt: generatedAt(),
				New: []model.DigestItem{{
					FindingID: 1, Kind: model.KindAdvisory, Tag: "[A]",
					Headline: headline, Lines: []string{line, hostile}, URL: findingURL,
				}},
				Open: 1,
			}
			if err := n.Send(t.Context(), d); err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			reqs := w.requests()
			if len(reqs) != 1 {
				t.Fatalf("webhook got %d requests, want 1", len(reqs))
			}
			if strings.Contains(string(reqs[0].body), "link_names") {
				t.Errorf("payload sets link_names, which turns plain @names into mentions: %s", reqs[0].body)
			}
			msg := decode(t, reqs[0].body)
			if msg.Text != d.Subject() {
				t.Errorf("text = %q, want only the subject %q", msg.Text, d.Subject())
			}
			var sections []string
			for _, b := range msg.Blocks {
				if b.Type == "section" && b.Text != nil {
					sections = append(sections, b.Text.Text)
				}
			}
			if len(sections) != 1 {
				t.Fatalf("section blocks = %d, want 1: %s", len(sections), reqs[0].body)
			}
			// The link to the finding is the notifier's own and the only
			// markup allowed in the section.
			body, ok := strings.CutSuffix(sections[0], "\n<"+findingURL+"|Open>")
			if !ok {
				t.Fatalf("section %q does not end with the finding link", sections[0])
			}
			if got, want := unescapeSlack(t, "section", body), "*[A] "+headline+"*\n"+line+"\n"+hostile; got != want {
				t.Errorf("section reads %q, want %q (escaping must not lose text)", got, want)
			}
		})
	}
}
