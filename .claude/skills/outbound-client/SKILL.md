---
name: outbound-client
description: How to write or change upstreamwatch code that talks to an outside service - the shared internal/httpx client, an HTTP adapter (GitHub issues, OSV, deps.dev, ecosyste.ms, go-get meta, KEV, EPSS, webhooks, ntfy), the SMTP notifier, or a git invocation in internal/gitsrc. Covers timeouts, the single retry layer, Retry-After, ETag conditional requests, GitHub rate limits and pagination, the https-only and private-address URL policy, tokens, and the Check method behind upw doctor. Triggers on "add an adapter", "call the GitHub API", "fetch from OSV", "ETag", "304", "rate limit", "retry", "SSRF", "private address", "git clone", "git fetch", "cat-file", "upw doctor", or any edit under internal/httpx, internal/gitsrc, internal/issues, internal/vuln, internal/upstream or internal/notify. Not for the LLM provider (use llm-triage), the http_cache table or CacheStore implementation (use sqlite-store), or how adapters are tested (use go-test).
---

# Outbound clients

upstreamwatch reaches the outside world through two doors: `internal/httpx` for HTTP and `internal/gitsrc` for the `git` binary. Every rule below exists because the inputs are untrusted: target repo URLs come from config, upstream URLs come from deps.dev and go-get meta tags, and every response body was written by someone else.

- Changing `internal/gitsrc` or anything that runs `git`: read [references/GIT-INVOCATION.md](references/GIT-INVOCATION.md) before writing code. It is short and every line is a security rule.
- The services, their auth, cadence and failure behaviour: `docs/external-sources.md`. The interfaces adapters implement: `docs/interfaces.md`. Where adapters sit and what they may import: the `upw-architecture` skill.

## Before you write an adapter

1. Find the service in `docs/external-sources.md`. If it is not there, you are adding a host to the outbound surface: add the entry in the same change and say so in your report, because the lead tracks that list as the egress allowlist.
2. The adapter implements the interface from `docs/interfaces.md` and nothing more. It never calls the store or another adapter; the service that calls it persists cursors and per-repo ETags. The one exception is httpx's own validator cache, below.
3. The adapter receives the shared client from its constructor. Do not call `http.Get`, use `http.DefaultClient`, or build an `http.Client` in an adapter, because that skips the URL policy, retries, pacing, body limits and timeout.

## The shared client: internal/httpx

One `*http.Client` per process, built once at startup and passed down.

- Transport: `http.DefaultTransport.(*http.Transport).Clone()`, which keeps connection pooling and HTTP/2. Replace `DialContext` with the policy dialer below. Set `Proxy` to nil, because a proxy dials on our behalf and the dialer would only ever see the proxy's address.
- `Client.Timeout` is non-zero. Pick it to cover the largest download (the EPSS CSV); an adapter that needs longer says so in its work order rather than building its own client.
- Response bodies are read through a size cap. Hitting the cap is an error, not a silent truncation.
- Logs carry method, host, path, status, duration and attempt number; never headers or bodies. Webhook URLs carry their secret in the path, so notifier requests are logged by host only.
- `CheckRedirect` caps hops and re-checks the scheme on each hop. `net/http` already drops `Authorization` when a redirect leaves the original domain; do not re-add headers in `CheckRedirect`.

### Retries live in httpx and nowhere else

Two retry layers multiply: three attempts in httpx times three in an adapter is nine requests against a rate limit.

- httpx retries `GET` and `HEAD` only, on transport errors and on 429, 502, 503 and 504, with bounded attempts and jittered exponential backoff. A 403 carrying `retry-after` or `x-ratelimit-remaining: 0` is GitHub's rate-limit answer and is handled like a 429.
- Honour `Retry-After` (seconds or HTTP date). If the wait it asks for is longer than httpx's maximum wait, do not sleep: return a typed rate-limit error carrying the reset time, so the job stops and its cursor stays where it is.
- Wait with a `select` on `ctx.Done()` and a timer, and check `ctx.Err()` before every attempt, so a cancelled run stops at once.
- httpx never retries `POST`. The LLM provider has exactly one retry layer of its own (see `llm-triage`); a failed digest is retried from the `digests` table on the next run.
- Adapters do not loop on errors. They return them, wrapped with what they were doing.

### Conditional requests

- Every repeated `GET` to a source that sends `ETag` or `Last-Modified` is conditional. httpx offers two modes: validators kept by httpx in `http_cache` through the `CacheStore` interface (`docs/data-access.md`), keyed by the exact URL, for feeds such as KEV, EPSS and OSV `modified_id.csv`; or a validator supplied and returned by the caller, for GitHub issue polling, whose ETag is stored with its cursor (next section).
- On 304 the adapter returns "not modified" and no items. The service then changes nothing: no cursor move, no upsert.
- Keep request URLs byte-identical between polls. Build query strings with `url.Values.Encode` (it sorts keys) and format timestamps one way: RFC 3339, UTC, whole seconds (`2006-01-02T15:04:05Z`). A differently encoded but equivalent URL misses the ETag and costs a counted request.
- Never put a token in a URL. URLs are cache keys and log fields.

### URL policy

Applied in the dialer, so it sees the address actually being connected to, after DNS resolution, on every redirect hop. A check at config load cannot catch a hostname that resolves differently later.

- Scheme `https` only, checked before the request and on each redirect. Reject URLs with userinfo.
- The dialer's `Control` function parses the resolved `ip:port`, unmaps IPv4-mapped IPv6, and rejects: loopback, private (RFC 1918 and `fc00::/7`), link-local unicast and multicast (this covers the `169.254.169.254` metadata address), multicast, unspecified, and `100.64.0.0/10`. That last range is where tailnet addresses live and `netip.Addr.IsPrivate` does not cover it; without it, an upstream URL could reach other machines on the tailnet.
- The only exception is the `allow_private_hosts` config setting for an endpoint the operator named (a self-hosted ntfy, a local model server). There is no code default and no bypass flag compiled into production paths.
- Tests: httptest servers listen on loopback, so tests construct httpx with a policy that permits loopback. The production constructor takes no such option.

### Tokens

- httpx holds no token. The adapter sets `Authorization` on each request from the token it was constructed with.
- Each adapter receives only the token it needs: the GitHub issues adapter gets the upstream token (no repository access); `gitsrc` gets, per target, the token that target's `auth` setting names, and none for `auth: none`. Mixing them sends a private-org credential to arbitrary public endpoints.
- Tokens never appear in logs, error strings, stored rows or URLs. Where an operator needs to see which token is in use, show the last four characters.

### Check, for upw doctor

Every outbound adapter implements the `Checker` interface in `docs/interfaces.md` (`Check(ctx context.Context) error`), which `upw doctor` calls.

- Make the cheapest request that proves reachability and auth. It must not spend money or counted rate budget where a free call exists: GitHub `GET /rate_limit` does not count against the limit and returns the remaining budget.
- Return an error an operator can act on: which host, which token (last four characters), what the status was.
- Never mutate anything. Use a short timeout.

## GitHub specifics

Endpoints and parameters are in `docs/external-sources.md` and PLAN.md section 5.6. How to call them:

- **Serial, with a gap.** All requests to `api.github.com` pass through one pacing gate in httpx with at least 100 ms between requests, because GitHub asks for serial requests to avoid secondary limits. No goroutine fan-out across repos.
- **Rate-limit headers.** Read `x-ratelimit-remaining` and `x-ratelimit-reset` on every response and return them to the service (they are shown on `/coverage`). When remaining drops below 200, stop the job early: commit what has been fetched, log remaining and reset, and return the typed rate-limit error. A 403 or 429 with `retry-after` is a secondary limit and follows the httpx rule above.
- **Pagination.** Follow the `Link` header's `rel="next"` URL exactly as given; do not rebuild it. Stop when there is no next link or when `backfill_max_issues` is reached. Only the first page is sent with `If-None-Match`.
- **Cursor.** `since` is the latest issue update time seen, minus 60 seconds for clock skew. Results come oldest first, so the service commits each page together with the cursor advanced to that page's maximum, in one transaction; stopping early loses nothing. The overlap means items arrive twice, so upserts are idempotent (keyed by repo and issue number, with `body_hash` deciding whether anything changed).
- **ETag.** Send `If-None-Match` only on the exact URL the ETag was issued for. Store the first page's ETag with the cursor it was requested with, and only when the listing reached its last page; after an early stop, clear it.
- **Pull requests** arrive in the issues list. Keep them, flagged `is_pull`.
- **Renamed repos** answer 301 and `net/http` follows. When `resp.Request.URL` differs from the URL you asked for, resolve the canonical `owner/name` once with `GET /repos/{owner}/{repo}` (`full_name`) and return it, so the service stores it and later polls go direct with stable URLs.
- **No token** means 60 requests an hour and 304s that count. Polling still works; config validation and `/coverage` warn, as PLAN.md 5.6 says.

## The SMTP notifier

Email uses `net/smtp` from the standard library, not httpx. The token, logging and `Check` rules above still apply.

- Dial with a `net.Dialer` that has a timeout, set a deadline on the connection, then build the client on it; `net/smtp` has no timeouts of its own.
- Require TLS: implicit TLS on port 465, otherwise `STARTTLS` before `AUTH`, and fail if the server does not offer it.
- The password comes from env through config and is never logged. `Check` connects, upgrades to TLS and authenticates, then quits without sending.

## Not covered here

- Testing adapters (httptest servers per API, recorded responses, fixture git repos): `go-test`.
- The `http_cache` and `kv` tables and the `CacheStore` implementation: `sqlite-store`.
- The LLM provider adapters and their retry and budget rules: `llm-triage`.
- Package placement and import rules: `upw-architecture`. Updating `docs/external-sources.md`: `docs-sync`.
- Reviewing this code for SSRF and token handling: `security-check`.
