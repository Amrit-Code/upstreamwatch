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
