# WO-08: Email (SMTP) and stdout notifiers

Milestone: M2
Agent: go-mech   Model: opus
Depends on: WO-05 (merged: `Digest.PlainText`, `Digest.Subject`)
Report file: `.claude/plans/m2/WO-08-notify-email-stdout.report.md`
Ticket: UPW-50, UPW-52 (stdout half)

## Objective

Two `report.Notifier` adapters: `internal/notify/email` sends the digest as a plain-text email over SMTP with the standard library, and `internal/notify/stdout` writes it to a writer. The email adapter also has `Check` for `upw doctor`. Serves M2 checklist items 9, 10, 11 and 12.

## Owned files

- `internal/notify/email/` (new package, all files, `testdata/`)
- `internal/notify/stdout/` (new package, all files)

Read-only: `internal/model/`, `internal/report/interfaces.go`, `go.mod`, `go.sum`. No new dependency: `net/smtp`, `crypto/tls`, `mime`, `mime/quotedprintable`, `net/mail` are enough.

## Interfaces

### Consumed

```go
// internal/report/interfaces.go
type Notifier interface {
	Send(ctx context.Context, d model.Digest) error
}
// internal/model/digest.go: Digest (and its PlainText and Subject methods, internal/model/digest_text.go)
// internal/model/secret.go: Secret (Reveal, String)
```

### Produced

```go
// internal/notify/stdout
func New(w io.Writer) *Notifier
func (n *Notifier) Send(ctx context.Context, d model.Digest) error

// internal/notify/email
type Options struct {
	Host     string
	Port     int // 465 means implicit TLS; anything else connects plain and upgrades with STARTTLS
	Username string
	Password model.Secret
	From     string
	To       []string
	Timeout  time.Duration // whole dial-to-quit exchange; default 30s
	// RootCAs, when set, replaces the system roots (tests use it with a
	// self-signed certificate).
	RootCAs *x509.CertPool
}
func New(opts Options, logger *slog.Logger) (*Notifier, error) // validates, does no I/O
func (n *Notifier) Send(ctx context.Context, d model.Digest) error
func (n *Notifier) Check(ctx context.Context) error
```

## Requirements

1. stdout `Send` writes `d.PlainText()` followed by one newline to `w`, as one `Write` call, guarded by a mutex; returns the write error.
2. email `New` rejects (error naming the field, never the password): empty host; port outside 1..65535; empty or unparseable `From` or any `To` (`net/mail.ParseAddress`); any CR or LF in host, username, from or to; an empty `To`. A username with an empty password, or a password with an empty username, is an error.
3. Message: headers `From`, `To` (comma-separated), `Subject` (`d.Subject()`, encoded with `mime.QEncoding` UTF-8 when it has non-ASCII bytes; the em dash makes this the normal case), `Date` (RFC 5322 from `d.GeneratedAt`), `Message-ID` (`<random hex>@<host part of From>`), `MIME-Version: 1.0`, `Content-Type: text/plain; charset=utf-8`, `Content-Transfer-Encoding: quoted-printable`, `Auto-Submitted: auto-generated`; body `d.PlainText()` encoded quoted-printable with CRLF line endings. A subject containing CR or LF after rendering is an error (header injection guard).
4. Transport: port 465 → `tls.Dial`; otherwise plain TCP, then `STARTTLS` when the server advertises it. Plain transport without TLS is allowed only when `Host` is a loopback address or `localhost` (so a local relay and the tests work); for any other host a server without STARTTLS is an error and the password is never sent. TLS uses `ServerName = Host`, minimum TLS 1.2, `RootCAs` when set. Authentication with `smtp.PlainAuth` only when a username is set. The whole exchange honours `ctx` and `Timeout` (set a deadline on the connection; close it when `ctx` ends).
5. `Send` delivers one message to all recipients (`RCPT TO` each). Errors name the host and port and the SMTP step (`email: smtp.example.com:587: auth: ...`) and pass through `model.Redact`; they never contain the password.
6. `Check`: dial, EHLO, STARTTLS as in 4, AUTH if a username is set, then QUIT, sending no message.
7. One info log line per send (recipient count and host), never the password or message body.

## Tests to include

A fake SMTP server in the test files (a goroutine on `127.0.0.1:0` speaking enough ESMTP: EHLO with optional STARTTLS and AUTH PLAIN, MAIL, RCPT, DATA, QUIT, recording what it received):

- `TestSend_Plain`: loopback host, no TLS, no auth → one message received with the headers of requirement 3, decoded body equals `PlainText()`, two recipients.
- `TestSend_STARTTLSAuth`: fake offers STARTTLS with a self-signed certificate (generated in the test) and AUTH PLAIN; `RootCAs` trusts it → upgraded before AUTH; the AUTH payload decodes to the username and password.
- `TestSend_NoTLSRefusedForRemoteHost`: options with host `smtp.invalid-example.test` resolved by a dial hook to the fake without STARTTLS → error, the fake never sees AUTH. (Add an unexported dial function field so the test can point a non-loopback name at the fake.)
- `TestSend_ErrorHasNoPassword`: fake rejects AUTH with 535 → error mentions auth and not the password.
- `TestNew_Validation` (table) and `TestSubjectEncoding` (non-ASCII subject is Q-encoded; CR/LF rejected).
- `TestCheck`: completes the exchange, sends no DATA.
- stdout: `TestSend` writes `PlainText()` plus newline.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/notify/email/ ./internal/notify/stdout/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Config keys (they exist in `internal/config`), wiring into `cmd/upw` and `upw doctor` (WO-13). Webhook and ntfy notifiers (WO-09).

## Skills and docs

Load with the Skill tool: `outbound-client`.
Read: `docs/decisions.md` (email decision), `docs/interfaces.md` (Notifier), PLAN.md section 6 (Daily digest).

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings

