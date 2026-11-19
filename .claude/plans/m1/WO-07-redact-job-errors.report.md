Status: DONE
Work order: .claude/plans/m1/WO-07-redact-job-errors.md
Files changed:
- internal/pipeline/redact.go (created)
- internal/pipeline/redact_test.go (created)
- internal/pipeline/runner.go (modified)
- internal/pipeline/runner_test.go (modified; two tests and the bytes/fmt imports added)
Commands run:
- `go test -race -count=1 ./internal/pipeline/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.481s
- `go test -race -run 'Redact' -v ./internal/pipeline/` -> exit 0 (subtest PASS lines and === lines filtered out below)
  --- PASS: TestRunner_RedactedErrorUnwraps (0.00s)
  --- PASS: TestRunner_RedactsJobError (0.00s)
  --- PASS: TestRedact (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.194s
- `make check` -> exit 0 (last 30 lines)
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.726s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.295s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.237s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.614s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ingest	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.243s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.191s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.569s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.537s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.482s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.031s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/timeline	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.232s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.221s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.324s
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
- `git status --porcelain` -> exit 0
   M internal/pipeline/runner.go
   M internal/pipeline/runner_test.go
  ?? .claude/plans/m1/WO-07-redact-job-errors.report.md
  ?? internal/pipeline/redact.go
  ?? internal/pipeline/redact_test.go
Decisions:
- The returned error is `fmt.Errorf("run job %s: %w", job, &redactedError{msg, jobErr})`; `redactedError` is a small unexported type in redact.go whose Error() is the redacted text and whose Unwrap() returns the job error (requirement 3).
- URL userinfo is matched as `scheme://` followed by a greedy run of characters other than `/ ? #` and whitespace, ending in `@`; so an unencoded `@` inside a password is redacted whole (`https://u:p@ss@h/x` -> `https://[REDACTED]@h/x`). The scheme is `[A-Za-z][A-Za-z0-9+.-]*` (RFC 3986), so `git+ssh://` is covered.
- "Non-space characters" read as `\S` (no whitespace of any kind); the gap between `Authorization:` and the scheme and between scheme and value, and after `Bearer`/`Basic`, is `[ \t]*` / `[ \t]+`, so a match never crosses a line.
- Token patterns are not anchored at word boundaries, following "wherever they appear" literally; e.g. `task-` followed by 20 key characters would be redacted from `sk-` on. `x-access-token:` is matched case-sensitively as written.
- Patterns apply in order: URL userinfo, GitHub tokens, sk- keys, Authorization header, Bearer/Basic, x-access-token. "[REDACTED]" matches none of them, so `Authorization: Bearer v` stays `Authorization: Bearer [REDACTED]` after the Bearer pattern rereads it.
- Test token values are built from repeated characters at runtime so no committed string looks like a real credential.
Outside scope: The doc paragraph in docs/architecture.md (after the second sequence diagram) says what a failed job writes but not that the text is redacted; docs-sync may want to add one sentence saying runs.error, the job_failure rank reason and the "job failed" log line hold the redacted error text.
Concerns: none
Question or blocker: none

## Gate (lead, 2026-11-19)

make check rerun in the worktree: exit 0. `go test -race -run Redact -v ./internal/pipeline/`: the three tests PASS. Diff inside owned files. Astra (gpt-6-astra, high) inspection: APPROVED, no findings. Accepted.
