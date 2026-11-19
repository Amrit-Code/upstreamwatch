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

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-08-notify-email-stdout.md
Files changed:
- internal/notify/email/email.go (modified)
- internal/notify/email/email_test.go (modified)
- internal/notify/email/fakesmtp_test.go (modified)
- .claude/plans/m2/WO-08-notify-email-stdout.report.md (modified)

Findings addressed:
1. Data race on `conn`: `session` now keeps the dialled connection in `raw`, which is never reassigned; the `context.AfterFunc` callback, the deadline and every cleanup close `raw` (closing the TCP connection also unblocks TLS reads). The TLS wrapper lives in a separate `conn` variable used only for `smtp.NewClient`. New test `TestSend_CancelDuringImplicitTLS`: the fake completes the implicit-TLS handshake, signals on a channel and then hangs; the test cancels the context while the client waits for the greeting and expects `context.Canceled`. Run against the unfixed code under `-race -count=3` it reported `WARNING: DATA RACE` (email_test.go:335/343 in the stacks); after the fix `-race -count=10` is clean.
2. QUIT after an accepted message: QUIT moved out of `session` into the callers. `Send` calls QUIT after `DATA` close returns success and, on failure, logs `email quit failed after the message was accepted` at Warn with host and the redacted step error, and returns nil (the Info "email digest sent" line still follows). `Check` returns the quit error as before. New fake option `dropAtQUIT` closes the connection without replying; new tests `TestSend_QUITFailureAfterAcceptedMessage` (Send returns nil, message received, WARN line names `: quit: `) and `TestCheck_QUITFailure` (Check returns a quit error). Against the unfixed code the Send test failed with `quit: EOF, want nil after the message was accepted`.
3. Redaction test that did not bite: the fake's 535 reply now echoes the decoded AUTH payload (`%q`, so the password appears verbatim). `TestSend_ErrorHasNoPassword` additionally asserts the error contains `[REDACTED]`; `TestCheck_AuthRejected` uses the same fake and so is now meaningful too. Mutation check: with the password replacement in `stepError` temporarily disabled, both tests failed (`... contains the password`); the line was restored and confirmed (`grep -n 'pw != ""'` shows it at email.go:284).

Also changed in the fake: implicit TLS now completes before the `hang` check, so `hang` with `implicitTLS` hangs after the handshake (the plain `hang` test is unaffected).

Commands run:
- `go test -race -count=3 ./internal/notify/email/` (new tests, before the fix) -> exit 1
  WARNING: DATA RACE ... testing.go:1867: race detected during execution of test
  --- FAIL: TestSend_CancelDuringImplicitTLS (0.01s)
  email_test.go:365: Send() error = email: 127.0.0.1:64540: quit: EOF, want nil after the message was accepted
  --- FAIL: TestSend_QUITFailureAfterAcceptedMessage (0.00s)
  (other FAIL lines in that run were race attributions to parallel tests; TestSend_ErrorHasNoPassword and TestCheck_AuthRejected pass alone: `go test -race -count=1 -run 'TestSend_ErrorHasNoPassword|TestCheck_AuthRejected' -v` -> exit 0)
- `go test -race -count=10 ./internal/notify/...` (after the fix) -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	2.302s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.112s
- `go test -race -count=1 -run 'TestSend_ErrorHasNoPassword|TestCheck_AuthRejected' ./internal/notify/email/` (password replacement temporarily disabled) -> exit 1
  --- FAIL: TestCheck_AuthRejected (0.01s)
      email_test.go:497: Check() error = "email: 127.0.0.1:64932: auth: 535 \"5.7.8 authentication credentials invalid: \\\"\\\\x00upw-sender\\\\x00example-password-7f3a\\\"\"" contains the password
  --- FAIL: TestSend_ErrorHasNoPassword (0.01s)
      email_test.go:284: Send() error = "... contains the password
      email_test.go:287: Send() error = "...", want the echoed password replaced by [REDACTED]
  FAIL
- `make check` -> exit 0
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
- `go test -race -count=1 ./internal/notify/email/ ./internal/notify/stdout/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.248s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.102s
- `git status --porcelain` -> exit 0 (before this section was appended)
   M internal/notify/email/email.go
   M internal/notify/email/email_test.go
   M internal/notify/email/fakesmtp_test.go

Decisions:
- QUIT is now each caller's last step inside the session callback rather than part of `session`, so Send can treat it as best effort and Check can keep it fatal.
- The Warn line carries the redacted `*smtpError` (`email: host:port: quit: ...`) as its `error` attribute; no password or body.
- `TestSend_CancelDuringImplicitTLS` asserts only `errors.Is(err, context.Canceled)`, not the step name: the cancel lands after the server-side handshake, which in practice is after the client's handshake returns, but the step could in principle be `tls` rather than `greeting`.

Outside scope: none new.
Concerns: none
Question or blocker: none
