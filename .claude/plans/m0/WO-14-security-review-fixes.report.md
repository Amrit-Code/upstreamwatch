Status: DONE
Work order: .claude/plans/m0/WO-14-security-review-fixes.md
Files changed:
- internal/web/hostcheck.go (created)
- internal/web/server.go (modified)
- internal/web/server_test.go (modified)
- internal/httpx/policy.go (modified)
- internal/httpx/client_test.go (modified)
- internal/store/db.go (modified)
- internal/store/db_test.go (modified)
Commands run:
- `make check` -> exit 0
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.271s
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
  (an earlier uncached run of the same target, before the last test-only edit, also exited 0 with every package ok)
- `go test -race -count=2 ./internal/web/ ./internal/httpx/ ./internal/store/ ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.324s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.790s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.981s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.717s
- `go test -count=1 -run TestRedirect ./internal/httpx/` with the stripping temporarily disabled (`if left && false`, reverted at once) -> exit 1
  --- FAIL: TestRedirect_CrossHostStripsHeaders (0.00s)
  --- FAIL: TestRedirect_StrippedStaysStripped (0.00s)
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/httpx	0.202s
- `git status --porcelain` -> exit 0 (before this report file was written)
   M internal/httpx/client_test.go
   M internal/httpx/policy.go
   M internal/store/db.go
   M internal/store/db_test.go
   M internal/web/server.go
   M internal/web/server_test.go
  ?? internal/web/hostcheck.go
Decisions:
- Middleware order is securityHeaders(hostCheck(crossOrigin(mux))): the host check is the first thing that decides, and the header-only middleware wraps it so its 421s carry CSP and nosniff (requirement A1 read that way).
- 421 body is `misdirected request: host not allowed` via http.Error (text/plain, nosniff).
- A Listen value that does not split as host:port (only reachable with Options{} in tests; config validates http.listen) contributes no allowed host, so requests are refused (fail closed). NewServer's error contract is unchanged.
- With Listen port 0, Serve builds a separate handler chain from ln.Addr() instead of mutating the shared one, so Handler() keeps its Options-based set and there is no data race; BaseURL's host is still included.
- Loopback Listen keeps the Listen value as given (lowercased) plus localhost:<port>; unspecified Listen yields 127.0.0.1, localhost and [::1] with the port, built with net.JoinHostPort.
- Redirect stripping is done in httpx's CheckRedirect (shared by Do and HTTPClient()). Because net/http copies the first request's headers onto every hop, the check looks at every hop in via[1:] plus the new request against via[0]'s host, which gives the sticky behaviour of B6. Referer, which net/http adds before CheckRedirect, is removed too on a foreign hop.
- Existing web handler tests send Host `127.0.0.1:0`, the Listen that newTestServer (fakes_test.go, not owned) sets; TestHealth_StoreError now sets Listen to the same value; TestNewServer_TrustsBaseURLOrigin sends Host `upw.example.net`.
- TestServe_PortZeroAllowsActualAddress uses its own client with keep-alives off: with the shared DefaultClient under -race a spare dialled connection held Shutdown for its 5-second StateNew grace (package took ~7 s instead of ~1.3 s).
- Store: tightenDir stats the parent after MkdirAll and chmods to 0700 only when a group/other bit is set; error text `open store: data directory <dir> must not be accessible to group or others: <err>`. Open's doc comment updated.
- Extra test rows beyond those listed: `:8080`, `[::]:8080`, `[::1]:8080`, a specific non-loopback listen, empty Host, BaseURL with port and path; store table also covers 0750 and 0702.
Outside scope:
- docs rows that may need docs-sync: the web UI's host check (421 for hosts other than the listen address / loopback names / base_url host) and the cross-host redirect header rule in httpx; the outbound-client skill line "net/http already drops Authorization when a redirect leaves the original domain" now understates what httpx does.
- TestServe_ShutsDownOnCancel (existing, owned) still uses http.DefaultClient and could hit the same 5-second Shutdown wait under load; it did not in the final runs, so it was left as is.

## Fix round 1

Status: DONE
Work order: .claude/plans/m0/WO-14-security-review-fixes.md
Files changed:
- internal/httpx/policy.go (modified)
- internal/httpx/client.go (modified)
- internal/httpx/client_test.go (modified)
Commands run:
- `make check` -> exit 0
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.489s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.524s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.221s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.219s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.215s
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
- `go test -race -count=3 ./internal/httpx/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	2.142s
- `go test -count=1 -run 'TestRedirect|TestDo_RedirectEndingIn304' -v ./internal/httpx/` with both fixes temporarily disabled (RoundTrip stripping guarded by `&& false`; `conditional` read from `hreq.Header`), both reverted at once -> exit 1
  --- FAIL: TestDo_RedirectEndingIn304 (0.00s)
      --- FAIL: TestDo_RedirectEndingIn304/cross_host_304_is_a_status_error (0.00s)
  --- FAIL: TestRedirect_JarCookiesStripped (0.00s)
      --- FAIL: TestRedirect_JarCookiesStripped/cross_port (0.00s)
      --- FAIL: TestRedirect_JarCookiesStripped/A_->_B_->_A (0.00s)
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/httpx	0.245s
- `git status --porcelain` -> exit 0
   M internal/httpx/client.go
   M internal/httpx/client_test.go
   M internal/httpx/policy.go
   M internal/store/db.go
   M internal/store/db_test.go
   M internal/web/server.go
   M internal/web/server_test.go
  ?? .claude/plans/m0/WO-14-security-review-fixes.report.md
  ?? internal/web/hostcheck.go
Decisions:
- WO14-R1: the mark is the redirect chain itself. net/http links every hop to the previous one through `Request.Response.Request`, and shallow copies of a request keep that link. `leftFirstHost(r)` walks the chain back to the first request and reports whether r or any earlier hop went to a different host:port (case-insensitive), so the mark carries through every later hop, including a hop back to A. `policyTransport.RoundTrip` runs after net/http adds cookie-jar cookies; on a marked hop it clones the request (a RoundTripper must not modify its input) and applies the same allowlist (`stripForeign`). CheckRedirect keeps its stripping, which now calls `stripForeign` too. A request Do or a caller builds has no `Response`, so it is never marked.
- WO14-R2: `outcome.conditional` now comes from `resp.Request.Header`, the request that got the final response (the stripped clone on a cross-host hop), not from the request Do built.
- Tests: `TestRedirect_JarCookiesStripped` (HTTPClient() with a `net/http/cookiejar` jar holding a cookie for A; rows A->B cross-port, A->B->A, and a same-host A->A control that keeps the cookie; also checks that the first request carried the jar cookie). `TestDo_RedirectEndingIn304` (ValidatorsCaller with ETag; cross-host A->B where B answers 304 gives `*StatusError` 304 and B received no If-None-Match; a same-host control still returns NotModified). `pathRecorder` gained a `statuses` map for the scripted 304.
Outside scope: none beyond round 0.

## Fix round 2

Status: DONE
Work order: .claude/plans/m0/WO-14-security-review-fixes.md
Files changed:
- internal/store/findings.go (modified)
- internal/store/findings_test.go (modified)
- internal/store/db_test.go (modified)
- internal/web/server.go (modified)
- internal/web/server_test.go (modified)
- internal/web/export_test.go (created; ownership granted by the coordinator's ruling of 2026-11-19)
- internal/config/validate.go (modified)
- internal/config/load_baseurl_test.go (created)
Commands run:
- `make check` -> exit 0
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.554s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.133s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.101s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.256s
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
- `go test -race -count=3 ./internal/web/ ./internal/store/ ./internal/config/ ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.319s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.801s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.121s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.913s
- `go test -count=1 -run 'TestServe_ClosesConnections|TestNewServer_BadBaseURL|TestLoad_BaseURLNeedsHostname|TestSchema_DepVersionDeleteBehaviour|TestDB_UpsertFinding_UpdatesOnlyDepVersionLink' ./internal/web/ ./internal/config/ ./internal/store/` with all four fixes temporarily reverted (no Close after the shutdown deadline, `u.Host == ""` in both base-URL checks, no `dep_version_id` in the upsert's update), then restored and confirmed with grep -> exit 1
  --- FAIL: TestNewServer_BadBaseURL (0.00s)
      --- FAIL: TestNewServer_BadBaseURL/empty_hostname_with_path (0.00s)
      --- FAIL: TestNewServer_BadBaseURL/empty_hostname_with_port (0.00s)
  --- FAIL: TestServe_ClosesConnectionsAfterShutdownDeadline (2.06s)
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/web	2.302s
  --- FAIL: TestLoad_BaseURLNeedsHostname (0.00s)
      --- FAIL: TestLoad_BaseURLNeedsHostname/http://:8080/upw (0.00s)
      --- FAIL: TestLoad_BaseURLNeedsHostname/https://:443 (0.00s)
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/config	0.188s
  --- FAIL: TestDB_UpsertFinding_UpdatesOnlyDepVersionLink (0.02s)
  --- FAIL: TestSchema_DepVersionDeleteBehaviour (0.02s)
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/store	0.252s
- `git status --porcelain` -> exit 0
   M internal/config/validate.go
   M internal/httpx/client.go
   M internal/httpx/client_test.go
   M internal/httpx/policy.go
   M internal/store/db.go
   M internal/store/db_test.go
   M internal/store/findings.go
   M internal/store/findings_test.go
   M internal/web/server.go
   M internal/web/server_test.go
  ?? .claude/plans/m0/WO-14-security-review-fixes.report.md
  ?? internal/config/load_baseurl_test.go
  ?? internal/web/export_test.go
  ?? internal/web/hostcheck.go
Decisions:
- WO04-R1: the upsert's `ON CONFLICT ... DO UPDATE` now also sets `dep_version_id = excluded.dep_version_id` (excluded is NULL when DepVersionID is zero, via nullID). Nothing else in the update list changed. UpsertFinding's doc comment says so. Tests: `TestSchema_DepVersionDeleteBehaviour` goes on past the interval delete. It recreates interval 8, acks the finding, re-upserts, and checks that the finding is relinked to 8 with the same ID, the same first-seen time and its acked state. New `TestDB_UpsertFinding_UpdatesOnlyDepVersionLink` re-upserts with repo, package and advisory left zero. Rows: new interval 8 is stored, and zero is stored as NULL. Repo, package, advisory and first-seen keep their values.
- WO07-001: `Server` has an unexported `shutdownTimeout` field, set to `defaultShutdownTimeout` (10 s) in NewServer. `export_test.go` exposes `SetShutdownTimeout` to the external test package. When Shutdown returns an error, Serve calls `srv.Close()` and returns the shutdown error, joined with any Close error. Serve also calls `Close` on the other exit path, where `srv.Serve` fails before ctx is cancelled, so no exit leaves connections open. `TestServe_ClosesConnectionsAfterShutdownDeadline` uses a 50 ms timeout. A HealthReader blocks on its request context, and a client that never times out keeps the request open. The test checks that Serve returns an error wrapping `context.DeadlineExceeded`, that the handler is released, and that the client call returns.
- WO07-002: both `web.baseOrigin` and `config.validate` now require a non-empty `u.Hostname()` in place of `u.Host`. New rows in `TestNewServer_BadBaseURL`: `https://:443` and `http://:8080/upw`. New `internal/config/load_baseurl_test.go` covers three valid URLs and three rejected ones (`https://:443`, `http://:8080/upw`, `https://`).
Outside scope:
- The coordinator says main has `_extra_test.go` and integration tests that this worktree lacks. If any of them asserts that a re-upsert leaves `dep_version_id` unchanged, it will fail after merge because of WO04-R1. I could not run them here.
- docs-sync candidates: UpsertFinding now relinks `dep_version_id` (docs/data-access.md, if it describes the upsert); http.base_url must have a hostname.
