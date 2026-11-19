Status: DONE
Work order: .claude/plans/m0/WO-12-store-http-cache.md
Files changed:
- internal/store/httpcache.go (created)
- internal/store/httpcache_test.go (created)
- internal/store/interfaces_test.go (modified)
Commands run:
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
  ?   	github.com/Amrit-Code/upstreamwatch/internal/httpx	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.076s
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
  (The first run failed in my own test table: two RoundTrip rows with no LastModified lacked wantNullLastM. Fixed the table; the run above is the rerun.)
- `go test -race -run 'HTTPValidator' -v ./internal/store/ | grep -c '^--- PASS'` -> exit 0
  5
- `git status --porcelain` -> exit 0
   M internal/store/interfaces_test.go
  ?? internal/store/httpcache.go
  ?? internal/store/httpcache_test.go
  (the report file is created after this run)
- `go test -race -count=1 -coverprofile=cover.out ./internal/store` then `go tool cover -func=cover.out` -> exit 0
  httpcache.go:16:	scanHTTPValidator	88.9%
  httpcache.go:37:	urlHost			75.0%
  httpcache.go:47:	GetHTTPValidator	83.3%
  httpcache.go:61:	SaveHTTPValidator	80.0%
  total:			(statements)		85.6%
Decisions:
- "None" for etag and last_modified is NULL (both columns are nullable), written through the existing nullText helper; read back as "".
- Status is stored as the given integer (0 stays 0, not NULL); a NULL status reads back as 0. The work order names only ETag and LastModified for the "none" mapping.
- A zero FetchedAt is stored as NULL through formatTime, like every other timestamp in the package.
- The upsert also sets updated_ts from db.now(), because that column is NOT NULL. The table has no created_ts column, so none is written.
- Error text uses the host only: not found is `http validator for host "<host>": not found`. A URL that does not parse gives an empty host. The same host-only form is used for SQL errors in both methods.
- An empty URL passed to GetHTTPValidator is not rejected. It finds no row and returns ErrNotFound. Only Save was required to reject an empty URL.
- interfaces_test.go: besides the one assertion line, I added the `internal/httpx` import line it needs to compile.
- InTx is covered by a row of TestDB_HTTPValidator_RoundTrip and not by a separate test, so the acceptance count of 5 holds.
Outside scope:
- The sqlite-store skill says every table has created_ts and updated_ts. The http_cache table in 0001_initial_schema.sql has only updated_ts. Docs and the migration agree with each other (docs/database.md lists no created_ts for http_cache), so nothing was blocked; the skill's general statement is inaccurate for this table.
- For docs-sync: `store.DB` now implements `httpx.CacheStore` for the http_cache table (docs/data-access.md CacheStore row: the kv half is still pending, M2).
Concerns: none
Question or blocker: none
