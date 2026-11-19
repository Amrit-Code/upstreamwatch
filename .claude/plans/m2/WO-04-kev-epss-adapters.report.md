Status: DONE
Work order: .claude/plans/m2/WO-04-kev-epss-adapters.md
Files changed:
- internal/vuln/kev/doc.go (created)
- internal/vuln/kev/feed.go (created)
- internal/vuln/kev/feed_test.go (created)
- internal/vuln/kev/testdata/kev.json (created)
- internal/vuln/epss/doc.go (created)
- internal/vuln/epss/feed.go (created)
- internal/vuln/epss/feed_test.go (created)
- internal/vuln/epss/cap_test.go (created)
- internal/vuln/epss/testdata/epss_scores.csv (created)
- internal/vuln/epss/testdata/epss_scores.csv.gz (created; `gzip -n` of epss_scores.csv)
Commands run:
- `go test -race -count=3 ./internal/vuln/kev/ ./internal/vuln/epss/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.140s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.253s
- `go test -race -coverprofile=... ./internal/vuln/kev/ ./internal/vuln/epss/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.224s	coverage: 94.3% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.236s	coverage: 93.5% of statements
- `make check` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	11.128s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.641s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.916s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.238s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.204s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ops	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.689s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.558s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.493s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	6.651s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	5.008s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.213s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/vuln	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.122s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.117s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.218s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.435s
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
- `go test -race ./internal/vuln/kev/ ./internal/vuln/epss/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	(cached)
- `git status --porcelain` -> exit 0 (run before this report was written)
  ?? internal/vuln/epss/
  ?? internal/vuln/kev/
Decisions:
- Error prefixes: `kev: fetch <url>: ...` / `epss: fetch <url>: ...` for HTTP failures, `<feed>: decode <url>: ...` for parse failures (malformed JSON, bad gzip, missing header, too many bad rows, decompressed cap), `<feed>: check <host>: ...` for Check.
- Check uses a 10-second timeout, copied from osv.Client.Check.
- EPSS row with the wrong number of fields: skipped and counted as malformed, like a bad float (csv.Reader with FieldsPerRecord = -1). A CSV syntax error (stray quote) is fatal. Blank lines are ignored by encoding/csv.
- "More than 1%" is skipped*100 > (good + skipped): 1 bad of 100 passes, 2 of 10 fails. A header with no rows returns zero signals and no error.
- NaN is rejected by the 0..1 check (written as !(v >= 0 && v <= 1)).
- EPSS CVE column is not validated (the work order specifies validation only for KEV).
- KEV: a body without a `vulnerabilities` key decodes to zero signals, not an error (only a JSON syntax/type error fails).
- Decompressed cap enforced by a streaming capped reader (unexported field `maxDecompressed`, 256 MiB set by New, lowered in an internal test); the CSV is parsed while decompressing rather than buffered.
- Gzip fixture committed as a binary file next to its plain-CSV source.
Outside scope: none

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-04-kev-epss-adapters.md
Finding: WO04-001 (full feed URL, including query and userinfo, interpolated into errors; hostOf returned raw input on parse failure).
Files changed:
- internal/vuln/kev/feed.go (modified): new `diagURL` (scheme://host/path only) used in the fetch and decode errors; `hostOf` and `diagURL` return the constant `(invalid URL)` when the URL does not parse or has no host; `hostOf` uses `u.Host`, which excludes userinfo.
- internal/vuln/kev/feed_test.go (modified): `TestKEVRefresh_ErrorsOmitURLSecrets` (fetch and decode errors x query credential with fragment, userinfo, malformed URL), `TestKEVRefresh_ErrorURLKeepsPath` (error names scheme, host and path, query dropped), `TestCheck_ErrorsOmitURLSecrets` (same three URLs).
- internal/vuln/epss/feed.go (modified): same change as kev.
- internal/vuln/epss/feed_test.go (modified): `TestEPSSRefresh_ErrorsOmitURLSecrets`, `TestEPSSRefresh_ErrorURLKeepsPath`, `TestCheck_ErrorsOmitURLSecrets`, mirroring kev.
Commands run:
- `go test -race -count=1 -v -run 'Secrets|KeepsPath' ./internal/vuln/kev/ ./internal/vuln/epss/` -> exit 1 on the first try. The failure was in the test, not the code: the leak marker "user" matched httpx's own refusal text "URL has userinfo", e.g.
  feed_test.go:264: Refresh() error = "kev: fetch http://127.0.0.1:59878/kev.json: GET 127.0.0.1:59878: URL has userinfo", want no userinfo, query or fragment
  Fixed by using userinfo `private-name:private-value` and checking for the markers "private-" and "frag".
- `go test -race -count=3 ./internal/vuln/kev/ ./internal/vuln/epss/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.288s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.302s
- `make check` -> exit 0
  ?   	github.com/Amrit-Code/upstreamwatch/internal/vuln	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.163s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.160s
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
- `go test -race ./internal/vuln/kev/ ./internal/vuln/epss/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	(cached)
- `git status --porcelain` -> exit 0 (before this section was appended)
   M internal/vuln/epss/feed.go
   M internal/vuln/epss/feed_test.go
   M internal/vuln/kev/feed.go
   M internal/vuln/kev/feed_test.go
Decisions:
- Placeholder text for an unparseable or hostless URL: `(invalid URL)`, used for both the diagnostic URL and the Check host.
- The diagnostic URL is computed when an error is built, not stored at construction; the raw URL is still what is requested.
- I did not rerun the new regression tests against the pre-fix code. The old code formatted `f.url` verbatim, so the query-credential cases would have contained `private-value`.
Outside scope: none
