Status: BLOCKED
Work order: .claude/plans/m2/WO-09-notify-webhooks-ntfy.md
Files changed:
- internal/notify/discord/doc.go (created)
- internal/notify/discord/notifier.go (created)
- internal/notify/discord/notifier_test.go (created)
- internal/notify/discord/testdata/TestDiscordSend_Golden.golden (created)
- internal/notify/slack/doc.go (created)
- internal/notify/slack/notifier.go (created)
- internal/notify/slack/notifier_test.go (created)
- internal/notify/slack/testdata/TestSlackSend_Golden.golden (created)
- internal/notify/ntfy/doc.go (created)
- internal/notify/ntfy/notifier.go (created)
- internal/notify/ntfy/notifier_test.go (created)
- .claude/plans/m2/WO-09-notify-webhooks-ntfy.report.md (created)

Commands run:
- `go test -race -count=1 -cover ./internal/notify/...` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.291s	coverage: 97.8% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	1.291s	coverage: 98.3% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.290s	coverage: 95.2% of statements
- `make check` -> exit 0
  (last 30 lines)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ops	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/vuln	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
    Off | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- `go test -race ./internal/notify/discord/ ./internal/notify/slack/ ./internal/notify/ntfy/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	(cached)
- `git status --porcelain` -> exit 0 (run before this report was written)
  ?? internal/notify/

Decisions:
- Error bodies: requirement 5's "<first 200 bytes of body>" is not implemented (see blocker). Status errors read `<pkg>: post to <host>: status <n>` (and `<pkg>: check <host>: status <n>` for Check).
- Redaction keeps the chain: errors are a small unexported `redactedError` whose text is `model.Redact(msg)` and whose `Unwrap` returns the httpx error, so `errors.Is(ctx.Err())` and `errors.AsType[*httpx.StatusError]` still work. Each package has its own copy, because adapters may not share code.
- "Loopback host" in New means `localhost` (any case) or an IP literal that `netip.Addr.IsLoopback` accepts. A private LAN address over http is refused.
- Discord embed budget: embeds are added in order until 10, or until the next one would bring title plus description runes to 5,500 or more. The items cut by the budget count in "… and N more". Descriptions are not shortened to fit.
- Discord truncation escapes first and cuts after. The cap counts the escaped runes, `…` included, and never splits an escape (`\*` or `@` plus ZWSP). The title cap covers `Tag + " "` too, and Tag is not escaped because it does not come from upstream.
- Discord heartbeat body is exactly `{"content": ...}`, with no `allowed_mentions`, following "sends only content" literally. The text holds nothing from upstream.
- Discord and Slack JSON are encoded with HTML escaping off, so the bodies and goldens contain `<`, `>` and `&` as written.
- Slack "counts line" is read as `Open: N, acked: N, snoozed: N.`. The context block holds it as one mrkdwn element, and "… and N more" (plus `" " + FindingsURL` when set) as a second element.
- Slack section: the lines part is left out when an item has no Lines. Tag and Headline are escaped together. The item URL is not escaped (it comes from config base_url). Section text is not capped at Slack's 3,000-character limit, because the work order names no cap.
- Slack heartbeat follows the general layout: a header block and a context block holding the counts line.
- ntfy body cap: the whole body, `…` included, is at most 4,000 bytes (cut at 3,997 on a rune boundary, then `…`).
- ntfy Tags: `white_check_mark` for a heartbeat, `warning` when `!d.IsEmpty()`, and no header otherwise. Content-Type is `text/plain; charset=utf-8`.
- ntfy health request: `SecretPath` is false, because `/v1/health` is not the topic URL.
- Check timeout is 10 seconds, the same as the kev and epss adapters. Check failures log nothing and the success line is at Debug, as in kev.
- Send log line: `digest sent` with `channel` (discord, slack or ntfy), `host` and `items` = len(New)+len(Changed). It is written on success only, because a failure is returned to the caller, which logs it.

Outside scope:
- `internal/httpx/client.go` `Do`, and `internal/httpx/errors.go` `StatusError`: a non-2xx body is drained and thrown away, so no adapter can report one (see blocker).
- Doc rows to update in docs-sync: `docs/interfaces.md` implementations table, where the Checker row should add `discord.Notifier`, `slack.Notifier` (URL validation only) and `ntfy.Notifier`. `docs/external-sources.md` could add the ntfy `/v1/health` probe and Discord's `GET` on the webhook.
- A self-hosted ntfy on a LAN address (PLAN.md section 6 suggests the same compose stack) needs https, because New allows http only on loopback. httpx's `AllowPrivateHosts` would allow http to that host, but the requirement 1 wording does not. The lead may want to reconcile the two.

Question or blocker:
Requirement 5 asks for `discord: post to discord.com: status 400: <first 200 bytes of body>`. That cannot be done inside the owned files. `httpx.Client.Do` (internal/httpx/client.go, `attempt`) reads a non-2xx body only into `io.Discard`, through `io.LimitReader(resp.Body, discardLimit)`, to free the connection. It returns `*httpx.StatusError`, which is

    type StatusError struct { Method, Host, Path string; Status int }

and has no body field. The adapters may not build their own `http.Client`, and httpx is read-only to this work order, so the body never reaches an adapter.
- What is done: all of requirements 1 to 4, 6 and 7, and requirement 5 apart from the body excerpt. Every listed test is written and passes, and make check passes.
- What would unblock it is one of these:
  (a) A lead-owned change to httpx: add `Body []byte` to `StatusError` (the first 200 bytes, or a capped excerpt) and fill it in `Client.statusError` from the drained body. Each notifier would then add `": " + string(se.Body)` before `model.Redact`. Callers that read `StatusError` (`osv`, `depsdev`, `github`, `kev`, `epss`) would be unaffected unless they print it. Note that an excerpt in `StatusError.Error()` would reach every adapter's error text; adding the field without changing `Error()` avoids that.
  (b) A ruling that drops the body excerpt from requirement 5 and accepts `discord: post to <host>: status <n>` as implemented now. Under this ruling the work is complete as it stands.
