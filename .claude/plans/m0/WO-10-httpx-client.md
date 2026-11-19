# WO-10: Shared httpx client

Milestone: M0 (ticket UPW-44, pulled forward from M2 because `upw doctor` needs it)
Agent: go-builder   Model: opus
Depends on: WO-01, WO-02 (merged)
Report file: `.claude/plans/m0/WO-10-httpx-client.report.md`
Ticket: UPW-44

## Objective

`internal/httpx` is the one door for outbound HTTP: every adapter gets its client from here, so the URL and address policy, timeouts, body limits, the single retry layer, per-host pacing, conditional requests and rate-limit accounting are written once. The inputs are untrusted (URLs from config, from deps.dev and from go-get meta tags), so the address policy is a security boundary.

## Owned files

- `internal/httpx/` (all files except `interfaces.go`; keep the package comment in `doc.go`)

Lead-owned, read-only for you (stop and report if one is wrong):

- `internal/httpx/interfaces.go`
- `internal/model/` (including `httpcache.go`)
- `go.mod`, `go.sum`, `.go-arch-lint.yml`, `Makefile`, `scripts/`

## Interfaces

### Consumed

```go
// internal/httpx/interfaces.go
type CacheStore interface {
	GetHTTPValidator(ctx context.Context, url string) (model.HTTPValidator, error) // wraps model.ErrNotFound if none
	SaveHTTPValidator(ctx context.Context, v model.HTTPValidator) error
}

// internal/model/httpcache.go
type HTTPValidator struct {
	URL, ETag, LastModified string
	FetchedAt               time.Time
	Status                  int
}
```

### Produced

```go
// internal/httpx

type Options struct {
	Timeout           time.Duration            // whole request including the body; default 2 minutes
	MaxBodyBytes      int64                    // response body cap; default 64 MiB
	MaxAttempts       int                      // attempts per retryable request; default 3
	MaxWait           time.Duration            // longest single wait between attempts; default 30 seconds
	UserAgent         string                   // default "upstreamwatch"
	AllowPrivateHosts []string                 // hostnames (no port) the operator named; exempt from the address and scheme policy
	MinGap            map[string]time.Duration // minimum gap between requests per host; default {"api.github.com": 100ms}
}

func New(cache CacheStore, opts Options, logger *slog.Logger) *Client

type ValidatorMode int
const (
	ValidatorsNone   ValidatorMode = iota // plain request
	ValidatorsCached                      // httpx keeps ETag and Last-Modified in CacheStore, keyed by the exact URL
	ValidatorsCaller                      // the caller supplies Request.ETag and stores Response.ETag itself
)

type Request struct {
	Method       string        // default GET
	URL          string
	Header       http.Header   // the adapter sets Authorization here; httpx holds no token
	Body         []byte
	Validators   ValidatorMode
	ETag         string        // ValidatorsCaller only
	MaxBodyBytes int64         // overrides Options.MaxBodyBytes when positive
	SecretPath   bool          // the URL path is a credential (webhooks): log and report the host only
}

type Response struct {
	Status      int
	Header      http.Header
	Body        []byte
	NotModified bool      // 304 to a conditional request; Body is empty
	ETag        string    // the response's ETag, if any
	URL         string    // the final URL after redirects
	RateLimit   RateLimit
}

type RateLimit struct {
	Known     bool      // the response carried x-ratelimit-remaining
	Remaining int
	ResetAt   time.Time // UTC; zero if the response gave none
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error)

// HTTPClient returns an *http.Client that applies the URL and address policy,
// the timeout and the redirect rules, but no retries, pacing or body cap. It
// exists for libraries that insist on their own client (the Anthropic SDK).
func (c *Client) HTTPClient() *http.Client

// StatusError is returned for a final response that is neither 2xx nor a 304
// to a conditional request.
type StatusError struct {
	Method string
	Host   string
	Path   string // empty when Request.SecretPath
	Status int
}

// RateLimitError is returned when the server asks for a wait longer than
// Options.MaxWait, or attempts run out on a rate-limit response.
type RateLimitError struct {
	Host    string
	ResetAt time.Time // UTC; when to try again
}

var (
	ErrBlockedAddress = errors.New("address not allowed")      // the address policy refused the connection
	ErrSchemeNotAllowed = errors.New("scheme not allowed")     // not https (or not http for an allowed private host)
	ErrBodyTooLarge   = errors.New("response body too large")
)
```

`*StatusError` and `*RateLimitError` implement `error`; callers reach them with `errors.AsType`.

## Requirements

Follow the outbound-client skill's section "The shared client: internal/httpx"; every bullet there is a requirement. The points below fix what it leaves open.

1. **Transport.** One `*http.Transport` cloned from `http.DefaultTransport`, `Proxy` nil, `DialContext` replaced by a dialer whose `Control` function applies the address policy to the resolved address of every connection, including each redirect hop and each address tried for a multi-address host.
2. **Address policy.** After unmapping IPv4-mapped IPv6, refuse loopback, private (RFC 1918 and `fc00::/7`), link-local unicast and multicast, multicast, unspecified, and `100.64.0.0/10`. A refusal surfaces from `Do` as an error for which `errors.Is(err, ErrBlockedAddress)` is true and which names the host but not the path.
3. **Scheme and URL shape,** checked before the request and on every redirect hop: `https` only; no userinfo; a host is required. Violations satisfy `errors.Is(err, ErrSchemeNotAllowed)` for the scheme, and a plain descriptive error for the rest.
4. **`AllowPrivateHosts`.** For a request whose URL hostname equals an entry (case-insensitive, exact match, no wildcards), the address policy is skipped and scheme `http` is also accepted. The exemption is decided per hop from that hop's hostname, so a redirect from an allowed host to another host gets the full policy. There is no other bypass, and no test-only constructor: tests use `httptest` servers with `AllowPrivateHosts: []string{"127.0.0.1"}`.
5. **Redirects.** At most 5 hops; each hop re-checked per requirements 2 to 4. Do not re-add headers that `net/http` drops on a cross-domain redirect.
6. **Timeout and body.** `Options.Timeout` bounds the whole exchange including reading the body. The body is read through the cap; exceeding it returns an error satisfying `errors.Is(err, ErrBodyTooLarge)` and no partial body.
7. **Retries.** Only `GET` and `HEAD` are retried, on transport errors (but never on a policy refusal, a context error or a body-too-large error) and on status 429, 502, 503, 504, and on a 403 that carries `retry-after` or `x-ratelimit-remaining: 0`. Attempts are capped at `MaxAttempts`. Waits are exponential from 500 ms with full jitter, capped at `MaxWait`; a `Retry-After` header (delta seconds or HTTP date) or, for a rate-limited 403/429 without it, `x-ratelimit-reset` (Unix seconds) sets the wait instead. If that requested wait exceeds `MaxWait`, do not sleep: return `*RateLimitError` with `ResetAt`. If attempts run out on a rate-limit response, return `*RateLimitError`; on another retryable status, `*StatusError`; on a transport error, that error wrapped. Waiting is a `select` on `ctx.Done()` and a timer; `ctx.Err()` is checked before every attempt. `POST` and every other method get exactly one attempt.
8. **Pacing.** Requests to a host listed in `MinGap` pass through one gate per host: they are serialised and each starts at least the gap after the previous one finished. Waiting at the gate honours `ctx`. Retries pass through the gate too.
9. **Conditional requests.**
   - `ValidatorsCached` (GET only; any other method is an error): look up the exact `Request.URL` in the `CacheStore`; send `If-None-Match` and `If-Modified-Since` for whichever validators are stored; on 304 return `NotModified: true` and leave the cache untouched; on 2xx save the response's `ETag` and `Last-Modified` (with `FetchedAt` from the client's clock and the status) when it has at least one of them. A `CacheStore` read error other than not-found, or a write error, fails the request.
   - `ValidatorsCaller`: send `If-None-Match: <Request.ETag>` when it is non-empty; return the response's ETag in `Response.ETag`; never touch the `CacheStore`.
   - A 304 to a request that sent no validator is a `*StatusError`.
10. **Result.** 2xx and conditional 304 return a `Response` and nil error. Any other final status returns a zero `Response` and `*StatusError`. `Response.RateLimit` is parsed from `x-ratelimit-remaining` and `x-ratelimit-reset` when present.
11. **Headers.** `User-Agent` is set from `Options.UserAgent` unless the request sets its own. httpx adds no `Authorization` and stores none.
12. **Logging.** One `Debug` line per attempt (`http request`, with `method`, `host`, `path`, `status`, `duration_ms`, `attempt`) and one `Warn` line per retry wait (`http retry`, with `method`, `host`, `status`, `wait_ms`, `attempt`). Never a header, a body, a query string or userinfo. With `SecretPath`, `path` is omitted everywhere, including in every error string httpx produces and in errors it wraps from `net/http` (a `*url.Error` carries the full URL: replace it).
13. **Errors never contain a token, a query string or, with `SecretPath`, a path.** Error strings name method, host and (unless secret) path.
14. **`HTTPClient()`** returns a client sharing the policy transport, `Options.Timeout` and the redirect rules, without retries, pacing or the body cap.
15. The clock is a `now func() time.Time` field and the jitter source is injectable from in-package tests, so tests do not sleep for real backoff durations.
16. `internal/httpx` imports only the standard library and `internal/model`.

## Tests to include

`httptest` servers; a hand-written fake `CacheStore`; a fake clock and zero-jitter source so no test sleeps more than a few milliseconds.

- Address policy as a pure table test over the refusal function: `127.0.0.1`, `::1`, `10.0.0.1`, `172.16.0.1`, `192.168.1.1`, `169.254.169.254`, `fe80::1`, `fc00::1`, `100.64.0.1`, `100.127.255.254`, `0.0.0.0`, `224.0.0.1`, `::ffff:10.0.0.1` refused; `8.8.8.8`, `100.63.255.255`, `100.128.0.0`, `2606:4700:4700::1111` allowed.
- `TestDo_BlocksLoopbackWithoutAllow`: an `httptest` server URL without `AllowPrivateHosts` → `ErrBlockedAddress` (and `ErrSchemeNotAllowed` for its `http` scheme when checked first; assert whichever the order in requirement 3 gives, and test the address refusal through a TLS test server).
- `TestDo_RedirectToBlockedHost`: an allowed host redirects to `http://localhost:<port>/` (a different hostname, not in the allow list) → refused.
- `TestDo_RejectsSchemeAndUserinfo`: `http://example.com`, `ftp://`, `file:///etc/passwd`, `https://user:pass@example.com`.
- `TestDo_RedirectLimit`.
- `TestDo_BodyCap`: a body one byte over the cap → `ErrBodyTooLarge`; exactly at the cap → ok.
- `TestDo_Timeout`: a handler that stalls → error within the timeout; `ctx` cancellation during a retry wait returns promptly with `ctx.Err()`.
- Retry table: 503 then 200 → 2 attempts; 503 ×3 → `*StatusError` after 3; 429 with `Retry-After: 1` → waits 1 s on the fake clock; `Retry-After` as an HTTP date; `Retry-After: 3600` → `*RateLimitError` with `ResetAt` about an hour ahead and exactly 1 attempt; 403 with `x-ratelimit-remaining: 0` and `x-ratelimit-reset` → rate-limit path; plain 403 → `*StatusError`, 1 attempt; 404 → 1 attempt; `POST` with 503 → 1 attempt.
- `TestDo_Pacing`: two requests to a paced host start at least the gap apart on the fake clock; an unpaced host is not delayed.
- `TestDo_ValidatorsCached`: first GET saves ETag and Last-Modified; second sends both headers and a 304 gives `NotModified` with the cache unchanged; a changed response replaces the validators; a store error fails the request; a `POST` in this mode is an error.
- `TestDo_ValidatorsCaller`: header sent, ETag returned, fake store never called.
- `TestDo_RateLimitHeadersParsed`.
- `TestDo_SecretPathNeverLeaks`: with `SecretPath`, for a success, a 500, a connection refused and a timeout, neither the captured log output nor the error string contains the path segment `s3cr3t-path`; and without `SecretPath` no error or log contains a query string value.
- `TestDo_AuthorizationNotLogged`.
- `TestHTTPClient_AppliesPolicy`: blocked without the allow list, works with it, no retry on 503.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=3 ./internal/httpx/` | `ok`, each run under 15 s |
| `go list -f '{{join .Imports "\n"}}' ./internal/httpx \| grep upstreamwatch` | only `.../internal/model` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Any adapter (OSV, deps.dev, GitHub, notifiers) and its `Check` (WO-11 and later milestones).
- The `CacheStore` implementation in `internal/store` (WO-12).
- GitHub pagination, cursors and the "remaining below 200" stop rule: those are the issues adapter's (M3); httpx only reports `RateLimit`.
- A config key for `allow_private_hosts`; `cmd/upw` passes an empty list until an adapter needs one.
- The SMTP notifier, which does not use httpx.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `outbound-client`.
Read: `docs/external-sources.md`; `docs/interfaces.md` (the paragraph on `Checker`); `mvp-plan/PLAN.md` section 11 "Security considerations" (arbitrary git URLs, outbound surface); `.claude/skills/go-test/templates/fake-http-servers.md`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: Tests reach `httptest` servers through `AllowPrivateHosts`, the same operator-facing exemption production has, so there is no test-only bypass. An allowed host may use plain `http`, because the plan's example of such a host is a local model server on `http://host:11434`.
- 2026-11-19: A non-2xx final status is an error (`*StatusError`), so adapters do not each re-implement status checks.
- 2026-11-19 (fix round 1): The address policy also refuses these, so that no special-purpose range is reachable: `0.0.0.0/8`, `192.0.0.0/24`, `198.18.0.0/15`, `240.0.0.0/4` (which includes `255.255.255.255`), and the documentation ranges `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` and `2001:db8::/32`. IPv6 addresses that embed an IPv4 address are judged by the embedded address: for `64:ff9b::/96` (NAT64) and `2002::/16` (6to4), extract the IPv4 address and refuse the connection if the policy refuses that IPv4 address; `64:ff9b:1::/48` (local-use NAT64) and `2001::/32` (Teredo) are refused outright. Extend the table test with one refused and one allowed case for each embedded form (`64:ff9b::a00:1` and `2002:a00:1::1` refused; `64:ff9b::808:808` and `2002:808:808::1` allowed) and the new IPv4 ranges with their boundary neighbours.
- 2026-11-19: `Options.Timeout` bounding each attempt rather than the whole retried exchange is accepted, as are the other choices listed under Decisions in the report.
- 2026-11-19: The outbound-client skill and the go-test fake-server template still describe a loopback-permitting test policy; this work order's ruling replaces that, and the lead corrects the skill text.
