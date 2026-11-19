Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m0/WO-10-httpx-client.md
Files changed:
- internal/httpx/client.go (created): Options, ValidatorMode, Request, Response, RateLimit, Client, New, Do, HTTPClient
- internal/httpx/errors.go (created): ErrBlockedAddress, ErrSchemeNotAllowed, ErrBodyTooLarge, StatusError, RateLimitError
- internal/httpx/policy.go (created): address policy, policy dialer, URL checks on every hop, redirect cap
- internal/httpx/pace.go (created): per-host pacing gate
- internal/httpx/retry.go (created): retry classification, Retry-After and x-ratelimit parsing, backoff, ctx-aware sleep
- internal/httpx/fakes_test.go (created): fake clock, fake CacheStore, captured logger, test client helper
- internal/httpx/policy_test.go (created)
- internal/httpx/retry_test.go (created)
- internal/httpx/client_test.go (created)
(doc.go and interfaces.go unchanged.)

Commands run:
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.165s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.191s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.444s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.174s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.189s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.202s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	1.962s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/web	[no test files]
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- `go test -race -count=3 ./internal/httpx/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	2.135s
  (three runs in 2.1 s in total; well under 15 s each)
- `go list -f '{{join .Imports "\n"}}' ./internal/httpx | grep upstreamwatch` -> exit 0
  github.com/Amrit-Code/upstreamwatch/internal/model
- `git status --porcelain` -> exit 0 (before this report file was written)
  ?? internal/httpx/client.go
  ?? internal/httpx/client_test.go
  ?? internal/httpx/errors.go
  ?? internal/httpx/fakes_test.go
  ?? internal/httpx/pace.go
  ?? internal/httpx/policy.go
  ?? internal/httpx/policy_test.go
  ?? internal/httpx/retry.go
  ?? internal/httpx/retry_test.go
- `go test -race -coverprofile=... ./internal/httpx` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.606s	coverage: 93.9% of statements

Tests written (all from "Tests to include", plus sanity cases): TestBlockedAddr (the 17 listed addresses plus `::`, `ff02::1`, zoned `fe80::1%en0`, `::ffff:8.8.8.8`), TestRefuseBlocked, TestCheckURL, TestRequestedWait, TestBackoffCeiling, TestFullJitter, TestNew_Defaults, TestDo_Success, TestDo_BlocksLoopbackWithoutAllow, TestDo_RedirectToBlockedHost (http://localhost gives the scheme refusal; https://localhost reaches the dialer and gives the address refusal), TestDo_RejectsSchemeAndUserinfo, TestDo_InvalidURLHidesIt, TestDo_RedirectLimit, TestDo_BodyCap, TestDo_Timeout (stalled headers, stalled body, cancel during retry wait), TestDo_Retries (15 rows, including the listed ones), TestDo_TransportErrorRetried, TestDo_Pacing (paced and serialised, unpaced, cancelled at the gate), TestDo_ValidatorsCached, TestDo_ValidatorsCaller, TestDo_RateLimitHeadersParsed, TestDo_SecretPathNeverLeaks (success, 500, 503 retried, connection refused, timeout; with SecretPath and with a query value), TestDo_AuthorizationNotLogged, TestHTTPClient_AppliesPolicy.

Decisions:
- Options.Timeout is http.Client.Timeout, so it bounds each attempt (connect, redirects, headers, body). It does not span retries. A retried GET can take up to MaxAttempts x (Timeout + MaxWait).
- A per-attempt client timeout counts as a transport error and is retried for GET/HEAD. Caller cancellation is detected with ctx.Err() and is never retried. A body-read error other than the cap is also a transport error.
- Every policy refusal (blocked address, scheme, userinfo, missing host, redirect limit) names method and host only, never the path. Requirement 2 asks this for the address refusal, and I applied it to all refusals. Other errors name method, host and (unless SecretPath) path. A URL with no host reads as `GET (no host)`.
- "Host" in logs, StatusError and RateLimitError is url.URL.Host, so it includes a non-default port. Pacing and AllowPrivateHosts match on the hostname without the port, case-insensitively.
- MinGap: nil or empty falls back to {"api.github.com": 100ms}. A non-empty map replaces the default rather than merging with it. Entries <= 0 are ignored.
- RateLimitError.ResetAt is the server's time when it gave one (Retry-After as seconds or a date, or x-ratelimit-reset as exact Unix seconds). When the attempts run out on a rate-limit response that gave no time, ResetAt is now plus the backoff ceiling for that attempt, so it is never zero.
- Retry-After is honoured on 502/503/504 as well. If it asks for longer than MaxWait, Do returns RateLimitError even for those statuses, following the literal text of requirement 7. If attempts run out on 502/503/504, Do returns StatusError.
- POST and the other non-retried methods never get a RateLimitError: any final non-2xx is a StatusError, 429 included.
- Whether a request "sent a validator" (for the 304 rule) is read from the outgoing headers (If-None-Match or If-Modified-Since), including any the caller set in Request.Header.
- An unparsable URL returns `<METHOD> request: invalid URL` without the parse error, because url.Parse errors quote the whole URL, query and secret path included.
- The body of a non-2xx response is drained up to 64 KiB (only so the connection can be reused) and discarded.
- The URL policy (scheme, userinfo, host) is checked once in Do before any store or gate work, and again in a RoundTripper wrapper that sees the first request and every redirect hop. Do and HTTPClient share that wrapper. CheckRedirect only caps the hops: 5 redirects are followed and a 6th is refused.
- The per-hostname exemption is applied in DialContext: an allowed hostname gets a plain dialer, any other gets the dialer whose Control applies the address policy. An address that Control cannot parse is refused (fail closed).
- A request holds the pacing gate from just before it is sent until its body is read and closed. It releases the gate during retry waits, and each retry passes through the gate again.
- ValidatorsCached with a nil CacheStore returns an error instead of panicking.

Outside scope:
- The `outbound-client` skill ("URL policy", last bullet) says tests build httpx "with a policy that permits loopback" and that the production constructor takes no such option. Ruling 2026-11-19 in this work order says tests use AllowPrivateHosts and that there is no test-only constructor. I followed the work order. The skill bullet, and `.claude/skills/go-test/templates/fake-http-servers.md` (which uses `httptest.NewTLSServer` and a `newTestHTTPX` "trusting srv.Certificate()"), should be updated. httpx has no exported way to trust a test certificate, so adapter tests (WO-11 and later) should use plain `httptest.NewServer` with `AllowPrivateHosts: []string{"127.0.0.1"}`. An allowed host may use http.
- WO-12 (CacheStore in internal/store): httpx wraps CacheStore read and write errors other than not-found into its own errors. If the store puts the URL into those error strings, a query string would reach httpx errors. I suggest the store keep the URL out of errors other than the not-found one, which httpx swallows.
- docs: `docs/interfaces.md` and `docs/architecture.md` do not record the httpx public API (Options, the validator modes, StatusError and RateLimitError). docs-sync may want a short entry. I found nothing in docs/ that contradicts this implementation.

Concerns:
- The address policy is exactly the set the work order lists. These ranges are not refused and are worth a security-review look: 0.0.0.0/8 other than 0.0.0.0, 255.255.255.255 and 240.0.0.0/4, 192.0.0.0/24, 198.18.0.0/15, and IPv6 prefixes that embed an IPv4 address (NAT64 64:ff9b::/96, 6to4 2002::/16). On a host with NAT64 or 6to4 routing, the embedded forms could reach a private IPv4 address. I did not add them because the work order fixes the list.
- A ValidatorsCached feed whose server sends neither ETag nor Last-Modified on a later 2xx keeps the older stored validators, as requirement 9 says ("when it has at least one of them"). That is harmless but means a stale entry can linger.

## Fix round 1

Status: DONE
Work order: .claude/plans/m0/WO-10-httpx-client.md (ruling "2026-11-19 (fix round 1)", quoted in the coordinator's message)
Files changed:
- internal/httpx/policy.go (modified): blockedAddr now also refuses 0.0.0.0/8, 192.0.0.0/24, 198.18.0.0/15, 240.0.0.0/4 (which includes 255.255.255.255), 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24, 2001:db8::/32, 64:ff9b:1::/48 (local-use NAT64) and 2001::/32 (Teredo). For 64:ff9b::/96 (NAT64) it judges the IPv4 address in the last 4 bytes, and for 2002::/16 (6to4) the IPv4 address in bytes 2 to 5, by the same policy. The 100.64.0.0/10 prefix moved into the same table.
- internal/httpx/policy_test.go (modified): TestBlockedAddr grows from 21 to 59 cases: each new IPv4 range at both edges, with its outside neighbours allowed. The exception is 240.0.0.0/4, whose lower neighbour 239.255.255.255 is multicast and therefore refused, and which has no upper neighbour. The table also covers each documentation range including 2001:db8::/32 against 2001:db9::1, NAT64 and 6to4 with refused embedded addresses (64:ff9b::a00:1, 64:ff9b::7f00:1, 2002:a00:1::1, 2002:c0a8:101::1) and allowed ones (64:ff9b::808:808, 2002:808:808::1), local-use NAT64, and Teredo at both edges against 2001:1::1.
Commands run:
- `go test -race -count=1 -run TestBlockedAddr -v ./internal/httpx/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.330s
  (59 subtests passed)
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.477s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/web	[no test files]
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- `go test -race -count=3 ./internal/httpx/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	2.125s
- `go list -f '{{join .Imports "\n"}}' ./internal/httpx | grep upstreamwatch` -> exit 0
  github.com/Amrit-Code/upstreamwatch/internal/model
- `git status --porcelain` -> exit 0
  ?? .claude/plans/m0/WO-10-httpx-client.report.md
  ?? internal/httpx/client.go
  ?? internal/httpx/client_test.go
  ?? internal/httpx/errors.go
  ?? internal/httpx/fakes_test.go
  ?? internal/httpx/pace.go
  ?? internal/httpx/policy.go
  ?? internal/httpx/policy_test.go
  ?? internal/httpx/retry.go
  ?? internal/httpx/retry_test.go
Decisions: local-use NAT64 and Teredo are checked before the embedded-address rule, so an address in 64:ff9b:1::/48 is refused even when its last 4 bytes are a public IPv4 address (tested with 64:ff9b:1::808:808). The two NAT64 prefixes do not overlap anyway.
Outside scope: none new.
Concerns: none. The first concern in the original report is resolved by this round. The second (validators kept when a later 2xx sends neither header) stands, as requirement 9 says.
