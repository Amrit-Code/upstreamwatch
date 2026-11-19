Status: DONE
Work order: .claude/plans/m0/WO-15-fake-token-values.md
Files changed:
- internal/config/load_test.go (modified)
- internal/model/secret_test.go (modified)
- .claude/plans/m0/WO-15-fake-token-values.report.md (created)
Commands run:
- `git grep -nE -e 'github_pat_[A-Za-z0-9]' -e 'sk-ant-[A-Za-z0-9]' -e 'discord(app)?\.com/api/webhooks/[0-9]' -- ':!.claude/plans/m0/WO-15-fake-token-values.md' ':!.claude/skills' ':!mvp-plan' ':!docs'` (before the change) -> exit 0
  internal/config/load_test.go:146:	const token = "<old fake value>"
  internal/config/load_test.go:229:	const literal = "<old fake literal>"
  internal/model/secret_test.go:73:	const value = "<old fake value>"
- same `git grep` (after the change) -> exit 1
  (no output)
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.008s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.697s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.618s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.904s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.542s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.582s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.917s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.825s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.741s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.013s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.218s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.214s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.309s
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
- `git status --porcelain` (before the report file was written) -> exit 0
   M internal/config/load_test.go
   M internal/model/secret_test.go
Decisions:
- `<old fake value>` -> `fake-token-abcdefgh1234` in both files: 23 characters as before, same last four (`1234`), so the `****1234` redaction assertions are unchanged.
- `<old fake literal>` -> `fake-token-literal`: 18 characters as before. It is still a non-`${...}` value, so every TestLoad_RejectsLiteralSecret row still exercises literal rejection, and the not-echoed check still applies. The value is a plain YAML scalar in every row.
- No assertion was edited; only the three constant values changed.
Outside scope: none. After the change the acceptance grep matches nothing anywhere in the tree, so no matches remain outside the owned files.
