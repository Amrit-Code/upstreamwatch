Status: DONE
Work order: .claude/plans/m2/WO-08-notify-email-stdout.md
Files changed:
- internal/notify/stdout/stdout.go (created)
- internal/notify/stdout/stdout_test.go (created)
- internal/notify/email/email.go (created)
- internal/notify/email/message.go (created)
- internal/notify/email/export_test.go (created)
- internal/notify/email/fakesmtp_test.go (created)
- internal/notify/email/email_test.go (created)
- internal/notify/email/message_test.go (created)
- .claude/plans/m2/WO-08-notify-email-stdout.report.md (created)

Commands run:
- `go test -race -count=5 ./internal/notify/...` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.672s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.112s
- `go test -race -coverprofile=... ./internal/notify/email ./internal/notify/stdout` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.322s	coverage: 87.6% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.190s	coverage: 100.0% of statements
- `make check` -> exit 0
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.042s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.265s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.340s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.202s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/findings	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	9.016s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.745s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.681s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.422s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.377s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.242s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.088s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ops	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.610s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.496s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.473s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	5.152s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.870s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.201s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/vuln	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.261s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.250s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.216s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.614s
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
- `go test -race ./internal/notify/email/ ./internal/notify/stdout/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	(cached)
- `git status --porcelain` -> exit 0 (run before this report was written)
  ?? internal/notify/
- `gofmt -l internal/notify` -> exit 0, no output

Decisions:
- stdout Send wraps the write error as `stdout: write digest: %w` (errors.Is still finds the writer's error), following go-code's wrap-with-context rule.
- email New returns every validation problem joined with errors.Join, each prefixed `email: ` and naming the field (`host`, `port`, `username`, `password`, `from`, `to[i]`); the password value is never included.
- Timeout <= 0 falls back to the 30s default (no config key sets it today; negative is treated like unset rather than rejected).
- Header From and To use the parsed `mail.Address.String()` form (display names quoted/encoded); MAIL FROM and RCPT TO use the bare address.
- Date header uses `time.RFC1123Z` of `d.GeneratedAt` in its own location (RFC 5322 form `Sun, 01 Mar 2026 08:00:00 +0100`).
- Message-ID is 16 bytes from crypto/rand, hex-encoded, `@` the domain of the From address.
- Port 465 dials through the injectable dial function and then runs `tls.Client(...).HandshakeContext(ctx)`, which is `tls.Dial` made context-aware and testable through the dial hook.
- EHLO name is `localhost` (the net/smtp default), so the machine's hostname is not disclosed.
- Loopback means exactly `localhost` or an IP for which `netip.Addr.IsLoopback` is true; net/smtp's PlainAuth independently refuses to send the password over plain text except to `localhost`, `127.0.0.1` or `::1`.
- Errors are an unexported `*smtpError` whose text is `email: host:port: step: cause`, with the literal password replaced by `[REDACTED]` and then passed through `model.Redact`; it unwraps to the cause. When the context has ended, `ctx.Err()` replaces the closed-connection error, so `errors.Is(err, context.Canceled)` works. Step names: dial, tls, greeting, ehlo, starttls, auth, mail, rcpt, data, quit.
- Test seam: unexported `dial` field on Notifier, set from tests through `export_test.go` (`email.SetDial`), so tests stay in the external package and assert `var _ report.Notifier = (*email.Notifier)(nil)`.
- No `testdata/` was needed: the fake SMTP server and the self-signed certificate are built in the test.
- Tests: stdout Send 4 cases; email New 15-row table plus an all-problems case; Send 8 tests (plain, STARTTLS+auth with one-log-line check, implicit TLS, no-TLS refused for remote host, auth 535 without password, untrusted certificate, server hang timeout, cancelled context); Check 3 tests (full exchange no DATA, no AUTH without username, auth rejected); encodeSubject 5-row table; buildMessage CRLF and header order; messageID.

Outside scope:
- The `outbound-client` skill's SMTP section says to fail when the server does not offer STARTTLS, with no loopback exception; the work order (requirement 4) allows plain transport to `localhost` and loopback IPs. I followed the work order, which is more specific; the skill text may want the exception added.
- Doc rows now out of date for docs-sync: `docs/interfaces.md` Checker implementations row ("built so far") can add `email.Notifier`; the implementations table could name the packages `internal/notify/email` and `internal/notify/stdout`.

Concerns: none
Question or blocker: none
