package discord_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// hostileTexts are upstream strings that try to ping people or to turn into
// a markdown link in a Discord message.
var hostileTexts = []string{
	"@everyone",
	"@here",
	"<@123456789012345678>",
	"<@!123456789012345678>",
	"<@&123456789012345678>",
	"<!channel>",
	"[click](https://evil.example)",
	"<https://evil.example|x>",
	`\[click\](https://evil.example)`,
	"[click]\n(https://evil.example)",
	"[click](<https://evil.example>)",
	"@​everyone",
	"<#123456789012345678>",
	"<t:1700000000:R>",
}

const zeroWidthSpace = '​'

// markdownSpecial are the characters the notifier must escape.
const markdownSpecial = "\\*_~`|><[]"

// checkNeutralised fails the test unless s, as Discord would read it, can
// form neither a mention, nor a markdown link, nor '<' markup: every '@' is
// followed by a zero-width space, and no '[', ']' or '<' is left unescaped.
// It returns s with the escapes removed, the text a reader sees.
func checkNeutralised(t *testing.T, field, s string) string {
	t.Helper()
	runes := []rune(s)
	var visible strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\\' && i+1 < len(runes) && strings.ContainsRune(markdownSpecial, runes[i+1]):
			visible.WriteRune(runes[i+1])
			i++
		case r == '@':
			if i+1 >= len(runes) || runes[i+1] != zeroWidthSpace {
				t.Errorf("%s %q: '@' at rune %d is not followed by a zero-width space, so a mention can form", field, s, i)
			}
			visible.WriteRune(r)
			if i+1 < len(runes) && runes[i+1] == zeroWidthSpace {
				i++
			}
		case strings.ContainsRune(markdownSpecial, r):
			t.Errorf("%s %q: unescaped %q at rune %d", field, s, r, i)
			visible.WriteRune(r)
		default:
			visible.WriteRune(r)
		}
	}
	return visible.String()
}

// TestDiscordSend_HostileTextCannotPingOrMaskLinks checks that upstream text
// can neither ping anyone (every '@' is broken by a zero-width space, and
// allowed_mentions is empty), nor form a masked markdown link (no unescaped
// '[' or ']'), nor open mention, channel or timestamp markup such as <#123>
// (no unescaped '<'). Bare URLs, which Discord auto-links with their real
// address, are defanged by the digest job before they reach the notifier.
func TestDiscordSend_HostileTextCannotPingOrMaskLinks(t *testing.T) {
	t.Parallel()
	for _, hostile := range hostileTexts {
		t.Run(hostile, func(t *testing.T) {
			t.Parallel()
			w := newWebhook(t, http.StatusNoContent, "")
			n := newNotifier(t, w, io.Discard)
			headline := "high  GHSA-xxxx  " + hostile
			line := "summary " + hostile
			d := model.Digest{
				GeneratedAt: generatedAt(),
				New: []model.DigestItem{{
					FindingID: 1, Kind: model.KindAdvisory, Tag: "[A]",
					Headline: headline, Lines: []string{line, hostile},
					URL: "https://upw.example.com/f/1",
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
			msg := decode(t, reqs[0].body)
			if msg.AllowedMentions == nil || msg.AllowedMentions.Parse == nil || len(msg.AllowedMentions.Parse) != 0 {
				t.Errorf("allowed_mentions = %+v, want {\"parse\": []}", msg.AllowedMentions)
			}
			if msg.Content != d.Subject() {
				t.Errorf("content = %q, want only the subject %q", msg.Content, d.Subject())
			}
			if len(msg.Embeds) != 1 {
				t.Fatalf("embeds = %d, want 1", len(msg.Embeds))
			}
			e := msg.Embeds[0]
			// The tag is the digest job's own marker; the headline after it
			// is upstream text.
			title, ok := strings.CutPrefix(e.Title, "[A] ")
			if !ok {
				t.Fatalf("title = %q, want it to start with the tag", e.Title)
			}
			if got, want := checkNeutralised(t, "title", title), headline; got != want {
				t.Errorf("title reads %q, want %q (escaping must not lose text)", got, want)
			}
			if got, want := checkNeutralised(t, "description", e.Description), line+"\n"+hostile; got != want {
				t.Errorf("description reads %q, want %q (escaping must not lose text)", got, want)
			}
			if e.URL != "https://upw.example.com/f/1" {
				t.Errorf("embed url = %q, want the finding URL", e.URL)
			}
		})
	}
}

// Cutting a long headline or description to its limit never splits an
// escape: wherever the cut falls, the kept text is still neutralised.
func TestDiscordSend_HostileTextAtTheCut(t *testing.T) {
	t.Parallel()
	const hostile = "[@everyone](https://evil.example)"
	for _, cut := range []struct {
		field string
		limit int // runes of upstream text the field keeps
	}{{"title", 252}, {"description", 1000}} {
		for pad := cut.limit - 12; pad <= cut.limit; pad++ {
			text := strings.Repeat("x", pad) + hostile
			w := newWebhook(t, http.StatusNoContent, "")
			n := newNotifier(t, w, io.Discard)
			item := model.DigestItem{FindingID: 1, Kind: model.KindAdvisory, Tag: "[A]", Headline: "high", Lines: []string{"x"}}
			if cut.field == "title" {
				item.Headline = text
			} else {
				item.Lines = []string{text}
			}
			if err := n.Send(t.Context(), model.Digest{GeneratedAt: generatedAt(), New: []model.DigestItem{item}}); err != nil {
				t.Fatalf("%s pad %d: Send() error = %v", cut.field, pad, err)
			}
			reqs := w.requests()
			if len(reqs) != 1 {
				t.Fatalf("%s pad %d: webhook got %d requests, want 1", cut.field, pad, len(reqs))
			}
			e := decode(t, reqs[0].body).Embeds[0]
			got := e.Description
			if cut.field == "title" {
				got = strings.TrimPrefix(e.Title, "[A] ")
			}
			if !strings.HasSuffix(got, "…") {
				t.Errorf("%s pad %d: %q was not cut", cut.field, pad, got)
			}
			checkNeutralised(t, cut.field, strings.TrimSuffix(got, "…"))
		}
	}
}
