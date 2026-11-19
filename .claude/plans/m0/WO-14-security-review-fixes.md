# WO-14: Fixes from the M0 security review

Milestone: M0
Agent: go-mech   Model: opus
Depends on: WO-04, WO-07, WO-10 (merged)
Report file: `.claude/plans/m0/WO-14-security-review-fixes.report.md`
Ticket: UPW-25

## Objective

Close three low-severity findings from the M0 security review before later milestones build on them: the web UI accepts any `Host` header (a DNS-rebinding path once POST endpoints exist), redirects can carry credential headers to another host, and an existing data directory keeps loose permissions.

## Owned files

- `internal/web/server.go`, `internal/web/server_test.go`, and a new `internal/web/hostcheck.go` if you want one
- `internal/httpx/policy.go`, `internal/httpx/client.go`, and the httpx test files
- `internal/store/db.go`, `internal/store/db_test.go`

Lead-owned, read-only for you: everything else, including every `interfaces.go`, `cmd/upw/`, `internal/config/`, `.go-arch-lint.yml`, `go.mod`.

## Interfaces

### Consumed and produced

No exported signature changes. `web.Options` keeps its two fields (`Listen`, `BaseURL`); `httpx` and `store` keep their APIs.

## Requirements

### A. Host check in `internal/web`

1. One middleware, outermost (before the security headers are skipped by nothing: the security headers must still be set on its rejections), rejects every request whose `Host` header is not allowed with status `421 Misdirected Request` and a short plain-text body. It logs one `Warn` line, `request rejected, host not allowed`, with the `host` attribute.
2. The allowed set is computed once in `NewServer` from `Options`, compared case-insensitively on the full `host[:port]` value of `r.Host`:
   - the `Listen` value itself, when its host part is a specific address or name (`127.0.0.1:8080`);
   - when the host part of `Listen` is empty or unspecified (`:8080`, `0.0.0.0:8080`, `[::]:8080`): `127.0.0.1:<port>`, `localhost:<port>` and `[::1]:<port>`;
   - when the host part of `Listen` is a loopback address: additionally `localhost:<port>`;
   - the host of `BaseURL`, exactly as it appears there (with its port if it has one, without if not), when `BaseURL` is set.
   Nothing else is allowed. There is no wildcard and no option to disable the check.
3. When `Listen` has port `0` (tests), the allowed set is built from the actual listening address inside `Serve`, before the first request is served. `Handler()` used without `Serve` (handler tests with `httptest`) builds the set from `Options` alone, so such tests set `Listen` to the host they send.
4. Existing behaviour is unchanged for allowed hosts, including the cross-origin protection and its trusted `BaseURL` origin.

### B. Redirects in `internal/httpx`

5. For both `Client.Do` and the client returned by `HTTPClient()`: when a redirect hop's URL host (`host[:port]`, case-insensitive) differs from the host of the first request in the chain, every request header is removed from the redirected request except `User-Agent`, `Accept`, `Accept-Encoding` and `Accept-Language`. This covers `Authorization`, `Cookie`, `X-Api-Key`, `If-None-Match`, `If-Modified-Since` and any other header an adapter or SDK set. Same-host redirects keep their headers as `net/http` leaves them.
6. Once headers have been stripped on a hop they stay stripped for the rest of the chain, including a later hop back to the original host.
7. The redirect limit, the per-hop URL policy and the address policy are unchanged.

### C. Data directory mode in `internal/store`

8. `Open` ensures the parent directory of the database path has mode `0700`: it creates a missing directory with that mode, as now, and when the directory already exists with any group or other permission bit set it calls `Chmod` to `0700`. If that `Chmod` fails, `Open` returns an error naming the directory and saying it must not be accessible to group or others. A directory that is already `0700` or stricter is left alone.
9. The database file mode rule is unchanged.

## Tests to include

- `TestHostCheck`: table over `Listen`/`BaseURL` combinations and request hosts → allowed or 421. Include: `127.0.0.1:8080` listen with hosts `127.0.0.1:8080`, `localhost:8080`, `LOCALHOST:8080` (allowed) and `attacker.example`, `127.0.0.1:9999`, `127.0.0.1` (rejected); `0.0.0.0:8080` with `127.0.0.1:8080` (allowed) and `10.0.0.5:8080` (rejected); `BaseURL: https://upw.example.ts.net` with host `upw.example.ts.net` (allowed) and `upw.example.ts.net:8443` (rejected).
- `TestHostCheck_RejectionKeepsSecurityHeaders`.
- `TestHostCheck_RebindingShape`: `Host: attacker.example`, `Origin: http://attacker.example`, `Sec-Fetch-Site: same-origin`, method POST to `/` → 421, not 403 or 405 (the host check runs first).
- `TestServe_PortZeroAllowsActualAddress`: `Listen: 127.0.0.1:0`; a real request to the logged address succeeds; a request to the same address with `Host: attacker.example` gets 421.
- Update existing web tests so they send an allowed host.
- `TestRedirect_CrossHostStripsHeaders`: two `httptest` servers (reach the second through a different allowed hostname or port so the hosts differ); the request sets `Authorization`, `X-Api-Key`, `Cookie`, `If-None-Match` and `Accept`; the second server sees only `Accept` and `User-Agent` of those. Run it for `Do` and for `HTTPClient()`.
- `TestRedirect_SameHostKeepsHeaders`.
- `TestRedirect_StrippedStaysStripped`: A → B → A; the final request to A carries no `X-Api-Key`.
- `TestOpen_TightensExistingDataDir`: an existing `0755` directory becomes `0700`; a `0700` directory is untouched; (skip on a platform where modes do not apply).

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=2 ./internal/web/ ./internal/httpx/ ./internal/store/ ./cmd/upw/` | `ok` for all four (`cmd/upw` is not yours to edit; if one of its tests now fails because of the host check, report BLOCKED with the failing test and the host it sent) |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Redacting job error text before it is stored (no change this milestone; the rule that adapters keep secrets and query strings out of errors stands).
- `govulncheck` (the lead runs it).
- Any POST endpoint.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `web-ui`, `outbound-client`.
Read: `internal/web/server.go`, `internal/httpx/policy.go`, `internal/httpx/client.go`, `internal/store/db.go` as merged.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: `tailscale serve` forwards the original `Host`, which is the `http.base_url` host; that is why `BaseURL`'s host is in the allowed set. An operator who reaches the UI by another name sets `http.base_url` to it.
