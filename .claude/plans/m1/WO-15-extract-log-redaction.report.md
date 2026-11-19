Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-15-extract-log-redaction.md
Files changed:
- internal/extract/library.go (modified)
- internal/extract/compose.go (modified)
- internal/extract/dockerfile.go (modified)
- internal/extract/library_test.go (modified)
- .claude/plans/m1/WO-15-extract-log-redaction.report.md (created)

Commands run:
- `git log --oneline -1` -> exit 0
  0a847e7 plans: WO-15 extractor log redaction
- `go test -race -count=1 -run 'LogRedacted' ./internal/extract/` (tests added, production code not yet changed) -> exit 1
  --- FAIL: TestParse_SkippedDockerActionLogRedacted (0.00s)
      --- FAIL: TestParse_SkippedDockerActionLogRedacted/token_userinfo (0.00s)
  --- FAIL: TestParse_SkippedLocalPackageLogRedacted (0.00s)
      --- FAIL: TestParse_SkippedLocalPackageLogRedacted/token_package_name (0.00s)
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/extract	0.268s
  (Expected: shows the token rows catch the missing redaction. The userinfo rows passed before the fix; see Concerns.)
- `make check` -> exit 0
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.233s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.312s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.455s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.212s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.823s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.613s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.775s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.253s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.378s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.553s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.581s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.478s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.304s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.100s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.245s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.245s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.530s
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
- `go test -race -count=1 -run 'LogRedacted' -v ./internal/extract/` -> exit 0
  --- PASS: TestParse_SkippedDockerActionLogRedacted (0.00s)
      --- PASS: TestParse_SkippedDockerActionLogRedacted/token_userinfo (0.00s)
      --- PASS: TestParse_SkippedDockerActionLogRedacted/userinfo (0.00s)
  --- PASS: TestParse_SkippedLocalPackageLogRedacted (0.00s)
      --- PASS: TestParse_SkippedLocalPackageLogRedacted/userinfo_in_source_url (0.00s)
      --- PASS: TestParse_SkippedLocalPackageLogRedacted/token_package_name (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.153s
- `git status --porcelain` -> exit 0 (run before this report file was written)
   M internal/extract/compose.go
   M internal/extract/dockerfile.go
   M internal/extract/library.go
   M internal/extract/library_test.go

Decisions:
- The four log sites in internal/extract: `package` and `uses` in library.go, and `image` in compose.go and dockerfile.go. Each value comes from file content, so each is now wrapped in `model.Redact`. The `path` attribute is the manifest path given to Parse and is left as it is (Requirement 1).
- The poetry test is named `TestParse_SkippedLocalPackageLogRedacted` so the acceptance `-run 'LogRedacted'` selects it.
- Each test is a two-row table: the work order's input, plus a token-shaped input that reaches the logged attribute intact. Without the second row neither test would fail if the redaction were removed (see Concerns). Both tests also assert that the skip line was logged, so a "no SECRET" pass cannot come from an empty buffer.
- No dedicated tests for the compose and Dockerfile `image` lines. The change there is the same one-call wrap, and the work order lists only the two tests.

Outside scope: none

Concerns:
- The work order's two example inputs do not exercise the redaction. A probe test, since deleted, showed why:
  - Docker: the manifests library (v0.14.0) splits `docker://user:SECRET@registry.example/img:1` at the colon. It returns `Name = "docker://user"` and puts the rest in Version, so the logged `uses` value never held the password, even before this change.
  - Poetry: the `local package skipped` line logs only the package name, never the `[package.source].url`, so a `user:SECRET@` in the source URL was never logged either.
  - The token rows (`docker://ghp_SECRET...@registry.example/img:1` as `uses`, and `ghp_SECRET...` as a poetry package name) failed before the fix and pass after it. They are what actually guards the redaction.
- A side effect of that split: for `docker://user:pw@host/img` the library logs the username (`docker://user`) unredacted. `model.Redact` matches URL userinfo only when an `@` follows, so the username is kept. That is a username, not a credential, so I left it as it is.

## Gate (lead, 2026-11-19)

make check rerun: exit 0; the LogRedacted tests pass. Concern accepted: the work order's examples did not reach the log; the agent added token-shaped rows that fail without the fix and pass with it. Production diff is four model.Redact wrappers on logged values, no behaviour change. Small fully specified change, no separate reviewer. Accepted.
