Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m0/WO-07-query-web.md
Files changed:
- internal/query/service.go (created)
- internal/query/service_test.go (created)
- internal/query/readers_test.go (created)
- internal/web/server.go (created)
- internal/web/handlers.go (created)
- internal/web/templates/layout.html (created)
- internal/web/templates/findings.html (created)
- internal/web/static/style.css (created)
- internal/web/fakes_test.go (created)
- internal/web/server_test.go (created)
- internal/web/testdata/findings_empty.golden (created)
- internal/web/testdata/findings_list.golden (created)

Commands run:
- `go test -race -count=1 ./internal/web -run TestFindingsPage_Golden -update` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.288s
  (both golden files read after generation: the `<script>`, `</pre>`, quotes and `&` in rank reasons are escaped; the empty state shows `No open findings.`)
- `make check` (first run) -> exit 2
  --- FAIL: TestHealth_Shape (0.00s)
      --- FAIL: TestHealth_Shape/two_runs_one_running (0.00s)
          server_test.go:102: body  lacks "finished_at":null
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/web	0.239s
  (test bug: the decoder had consumed the recorder body before the substring check; fixed by reading the body once)
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
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/sched	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/store	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.366s
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
  (exit status confirmed separately: `make check >/dev/null 2>&1; echo $?` printed `make check exit 0`)
- `go test -race ./internal/web/ ./internal/query/` (run with -count=1) -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.247s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.132s
- `go test -race -count=3 -cover ./internal/web/ ./internal/query/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.366s	coverage: 86.0% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.247s	coverage: 100.0% of statements
- `go list -f '{{join .Imports "\n"}}' ./internal/web ./internal/query | grep upstreamwatch | sort -u` -> exit 0
  github.com/Amrit-Code/upstreamwatch/internal/model
- `git status --porcelain` -> exit 0
  ?? internal/query/readers_test.go
  ?? internal/query/service.go
  ?? internal/query/service_test.go
  ?? internal/web/fakes_test.go
  ?? internal/web/handlers.go
  ?? internal/web/server.go
  ?? internal/web/server_test.go
  ?? internal/web/static/
  ?? internal/web/templates/
  ?? internal/web/testdata/
  (run before this report file was written; the report file is the only addition since)

Decisions:
- Page title is `<title>Findings - upstreamwatch</title>` (view field `Title` plus the product name); the page has an `<h1>Findings</h1>`.
- Generic error bodies are `internal server error` (via `http.Error`, so `text/plain`), for the findings-page 500 and template failures.
- Malformed `BaseURL` uses the same rule as `internal/config/validate.go`: url.Parse must succeed, scheme http or https, non-empty host. The trusted origin is `scheme://host[:port]`; any path is dropped. The error text does not echo the URL.
- `http.Server.ErrorLog` is set to `slog.NewLogLogger(logger.Handler(), slog.LevelWarn)` so net/http's own diagnostics go through the injected logger rather than the `log` package (go-code logging rule). Not named in the work order.
- Log lines: `health check failed`, `load findings failed`, `render page failed` (with `page`), `encode json response failed`, all Error level with an `error` attribute, matching `internal/pipeline` style.
- Score formatting is Go's `%.1f`, which rounds half to even on the binary value: 7.25 renders `7.2`, 9.96 renders `10.0` (both in the golden file).
- Health times: `t.UTC().Truncate(time.Second).Format(time.RFC3339)`; `finished_at` is null when `FinishedAt.IsZero()`.
- Rows are preformatted into a page view struct in Go; no FuncMap was needed.
- The rank-reason cell has class `reason` styled `white-space: pre-wrap`, per the web-ui escaping guidance; rank reason is rendered with a plain `{{.RankReason}}` action.
- Extra sanity tests beyond the list: `TestStylesheet` (content type), `TestNewServer_TrustsBaseURLOrigin` (trusted origin passes the cross-origin check, then 405), `TestServe_ListenError`, and an unknown `/static/other.css` 404 row.
- Golden helper is a local `assertGolden` in `internal/web/fakes_test.go`, because `internal/testutil/golden` does not exist yet and `internal/testutil/` is not an owned file.

Outside scope:
- The go-test golden template refers to a shared helper `internal/testutil/golden`; it does not exist in the tree. When it is added, `internal/web/fakes_test.go`'s `assertGolden` can be replaced by it.
- Docs rows for docs-sync: `docs/architecture.md` "HTTP routes" does not list `GET /static/style.css`.

Concerns:
- `query.Service` stores the `logger` it is given but never uses it, because requirement 1 says the methods add no logic and errors are returned, not logged. The constructor signature requires it, so it is kept; a reviewer may flag the unused field.
