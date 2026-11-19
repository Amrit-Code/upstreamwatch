# WO-09: Discord, Slack and ntfy notifiers

Milestone: M2
Agent: go-mech   Model: opus
Depends on: WO-05 (merged: `Digest.PlainText`, `Digest.Subject`)
Report file: `.claude/plans/m2/WO-09-notify-webhooks-ntfy.report.md`
Ticket: UPW-51, UPW-52 (ntfy half)

## Objective

Three `report.Notifier` adapters over the shared `httpx` client: `internal/notify/discord` and `internal/notify/slack` post the digest to an incoming webhook, `internal/notify/ntfy` publishes it to an ntfy topic URL. Each has `Check`. Serves M2 checklist items 9 and 11.

## Owned files

- `internal/notify/discord/`, `internal/notify/slack/`, `internal/notify/ntfy/` (new packages, all files, `testdata/`)

Read-only: `internal/model/`, `internal/report/interfaces.go`, `internal/httpx/`, `go.mod`, `go.sum`.

## Interfaces

### Consumed

`report.Notifier` (`Send(ctx, model.Digest) error`); `model.Digest`, `model.DigestItem`, `Digest.PlainText()`, `Digest.Subject()`; `model.Secret`; `httpx.Client.Do`, `httpx.Request` (set `SecretPath: true` for every request to a webhook or topic URL, because the URL is a credential), `*httpx.StatusError`.

### Produced (same shape in each package)

```go
func New(hc *httpx.Client, url model.Secret, logger *slog.Logger) (*Notifier, error) // validates the URL, no I/O
func (n *Notifier) Send(ctx context.Context, d model.Digest) error
func (n *Notifier) Check(ctx context.Context) error
```

## Requirements

1. `New` requires an absolute `https` URL (an `http` URL only when the host is loopback, for tests and a local ntfy). The URL never appears in errors or logs except as its host.
2. Discord: `POST` JSON `{"content": <header>, "embeds": [...], "allowed_mentions": {"parse": []}}`. The header is `d.Subject()`. One embed per item for the first 10 items of New then Changed: `title` = `Tag + " " + Headline` (max 256 runes, truncated with `…`), `description` = Lines joined by newline (max 1,000 runes), `url` = item URL when set. When items were left out (over 10, or `MoreNew`/`MoreChanged`), the content gets a second line `… and N more` plus the findings URL. A heartbeat sends only `content` with the subject and the plain-text body's last two lines. Upstream text (Headline, Lines) has Discord markdown neutralised: a backslash before each of ``\ * _ ~ ` | >`` and `[` `]`, and `@` followed by a zero-width space. Total embed characters stay under 5,500.
3. Slack: `POST` JSON `{"text": <subject>, "blocks": [...]}` with a `header` block (plain_text subject), then for each of the first 20 items a `section` block with `mrkdwn` text `*<tag headline>*\n<lines>\n<url|Open>` (URL part only when set), then a `context` block with the counts line, and `… and N more` when items were left out. Upstream text is escaped for Slack (`&` → `&amp;`, `<` → `&lt;`, `>` → `&gt;`), which is what stops injected links and mentions; Slack's emphasis characters need no escaping. Max 50 blocks.
4. ntfy: `POST` to the topic URL with body `d.PlainText()` truncated to 4,000 bytes on a UTF-8 boundary (with `…` appended when cut), headers `Title` = subject with non-ASCII characters replaced (`—` → `-`, others dropped), `Click` = `d.FindingsURL` when set, `Tags` = `warning` when there are items, `white_check_mark` for a heartbeat, `Priority` = `default`.
5. A non-2xx answer is an error (`discord: post to discord.com: status 400: <first 200 bytes of body>`). Errors pass through `model.Redact`.
6. `Check`: Discord `GET` the webhook URL (Discord answers 200 with the webhook's metadata and posts nothing); ntfy `GET <scheme>://<host>/v1/health` and require `{"healthy":true}`; Slack has no read-only probe, so `Check` validates the URL only and returns nil without a request (document this on the method).
7. One info log line per send naming the channel kind, host and item count.

## Tests to include

Against `httptest` servers through an `httpx` client that allows loopback:

- `TestDiscordSend_Golden`: a digest with 12 items (one Headline containing `@everyone` and `[x](http://evil)`) → request body equals a golden JSON: 10 embeds, mentions disabled, markdown neutralised, "and N more" line.
- `TestDiscordSend_Heartbeat`, `TestDiscordCheck`.
- `TestSlackSend_Golden`: escaping of `<https://evil|click>` and `&`; block count; `TestSlackCheck` makes no request.
- `TestNtfySend`: headers and truncated body; `TestNtfyCheck` (healthy and unhealthy).
- `TestSend_StatusError` for each: 500 → error naming the host, not the path.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/notify/discord/ ./internal/notify/slack/ ./internal/notify/ntfy/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Email and stdout (WO-08). Wiring and doctor (WO-13).

## Skills and docs

Load with the Skill tool: `outbound-client`.
Read: PLAN.md section 6 (Daily digest, Channel recommendation); `docs/external-sources.md`.

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings

