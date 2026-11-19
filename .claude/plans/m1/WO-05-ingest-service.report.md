Status: DONE
Work order: .claude/plans/m1/WO-05-ingest-service.md
Files changed:
- internal/ingest/doc.go (created)
- internal/ingest/service.go (created)
- internal/ingest/service_test.go (created)
- .claude/plans/m1/WO-05-ingest-service.report.md (created)

Commands run:
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.670s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.265s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.218s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.582s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.099s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.258s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.203s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.585s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.593s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.433s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.854s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/timeline	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.205s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.201s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.297s
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
- `go test -race -count=1 -v ./internal/ingest/` -> exit 0 (PASS/ok lines)
  --- PASS: TestName (0.00s)
  --- PASS: TestRun_CancelledBeforeStart (0.00s)
  --- PASS: TestRun_ScopeUnknownRepo (0.00s)
  --- PASS: TestRun_NoTargets (0.00s)
  --- PASS: TestRun_ScopeOneRepo (0.00s)
  --- PASS: TestRun_OneTargetFails (0.00s)
  --- PASS: TestNewService_CopiesTargets (0.00s)
  --- PASS: TestRun_AllTargetsSucceed (0.00s)
  --- PASS: TestRun_UpsertFails (0.00s)
  --- PASS: TestNewService_DefaultClock (0.00s)
  --- PASS: TestRun_ContextCancelled (0.00s)
  --- PASS: TestRun_AllTargetsFailListsEachInOrder (0.00s)
  --- PASS: TestRun_RecordFetchFails (0.00s)
      --- PASS: TestRun_RecordFetchFails/after_failed_sync (0.00s)
      --- PASS: TestRun_RecordFetchFails/after_successful_sync (0.00s)
  --- PASS: TestRun_TokenNotLogged (0.00s)
      --- PASS: TestRun_TokenNotLogged/text (0.00s)
      --- PASS: TestRun_TokenNotLogged/json (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.113s
- `go test -race -coverprofile=<scratch>/cover.out ./internal/ingest` then `go tool cover -func` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.213s	coverage: 100.0% of statements
- `git status --porcelain` -> exit 0 (before this report file was written)
  ?? internal/ingest/doc.go
  ?? internal/ingest/service.go
  ?? internal/ingest/service_test.go

Decisions:
- Sync receives a fresh TargetRepo holding exactly the stored ID plus the configured Slug, URL, Branch, Token and AllowPrivateHost (other stored fields are not passed; Sync does not read them).
- The failed-target error in requirement 7 is the Sync error text unchanged; UpsertRepo and RecordFetch errors are prefixed "upsert repo: " and "record fetch: "; when Sync and RecordFetch both fail the text is "<sync error> (record fetch: <error>)" so the "; " separator stays unambiguous.
- An UpsertRepo failure skips Sync and RecordFetch for that target (no repo ID to record against).
- The returned run error is an unexported type with Unwrap() []error, so errors.Is reaches each target's error; its text is exactly requirement 7.
- Cancellation: ctx.Err() is checked before each target, including the first; Run returns "ingest stopped before target <slug>: <ctx error>" (wraps ctx.Err(), errors.Is works) together with the stats gathered so far.
- Unknown slug error text: `ingest: no target with slug "<slug>"`; stats still carry all four keys at zero.
- Log lines (logger carries job=ingest via With): Warn "target sync failed" with slug and error for any failed target (sync, upsert or record); Info "target synced" with slug, branch, head, cloned.
- NewService copies opts.Targets so a caller mutating its slice later does not change the job.
- Test cases beyond the listed six: TestName, AllTargetsFailListsEachInOrder, UpsertFails, RecordFetchFails (2 rows), NoTargets, CancelledBeforeStart, NewService_DefaultClock, NewService_CopiesTargets.

Outside scope:
- The worktree started at 4dff154, one commit behind the M1 groundwork (0b4f89a); the lead fast-forwarded it during the run, and all commands above ran after that.
- Doc rows for docs-sync: docs/architecture.md job table, ingest row lists "URL and host policy" as ingest's pure decision, but per WO-01 that policy lives in internal/gitsrc; ingest itself makes no pure decision beyond scope selection. docs/data-access.md should list ingest as the user of RepoStore (UpsertRepo, RecordFetch). The "After M0 these packages exist" paragraph will need internal/ingest once M1 lands.
- Requirement 3's RecordFetch runs on the caller's context; if ctx is cancelled during a Sync, that attempt's RecordFetch will likely fail too and is reported as part of the target's error. Not changed (not requested).

Concerns: none
Question or blocker: none

## Fix round 1

Status: DONE
Work order: .claude/plans/m1/WO-05-ingest-service.md (findings ING-001, ING-002, ING-003; worktree at e5de08a)
Files changed:
- internal/ingest/service.go (modified)
- internal/ingest/service_test.go (modified)
- .claude/plans/m1/WO-05-ingest-service.report.md (modified)

Fixes:
- ING-001: model.Redact is applied to FetchResult.Error, the error attribute of both warn log lines, and each target's text in the run error (runError.Error). New test TestRun_RedactsCredentialsInSyncError uses a Sync error holding `https://deploy:<password>@...` and a `github_pat_` token. It checks the captured log, the RecordFetch input and the returned error text: none contains either secret, each contains `[REDACTED]`, and the RecordFetch Error is exactly `fetch https://[REDACTED]@git.example/example-org/alpha: server rejected [REDACTED]`.
- ING-002: RecordFetch now runs on `context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)`, where recordTimeout is 10s. Cancellation is still returned: ctx.Err() is checked before each target, as before, and once more after the loop, so being cancelled during the last target also returns `ingest stopped: <ctx error>`. fakeRepos has a `ctxAware` mode that fails RecordFetch when its context is done, and it records whether each context had a deadline. New tests: TestRun_CancelledDuringSyncStillRecords (the cancelled attempt is recorded on a context with a deadline, beta is not attempted, and the error wraps context.Canceled) and TestRun_CancelledDuringLastTarget.
- ING-003: syncTarget now also returns `synced` (whether Sync succeeded). Run counts fetched and cloned for every successful sync, and counts failed as well when recording fails. TestRun_RecordFetchFails now expects `targets 1, fetched 1, cloned 1, failed 1` after a successful sync with Cloned, and `1, 0, 0, 1` after a failed sync.

Commands run:
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.630s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.244s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.191s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.553s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.106s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.241s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.214s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.503s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.568s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.383s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.762s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/timeline	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.214s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.206s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.282s
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
- `go test -race -count=1 -v ./internal/ingest/` -> exit 0
  --- PASS: TestNewService_CopiesTargets (0.00s)
  --- PASS: TestRun_CancelledBeforeStart (0.00s)
  --- PASS: TestRun_CancelledDuringLastTarget (0.00s)
  --- PASS: TestRun_AllTargetsFailListsEachInOrder (0.00s)
  --- PASS: TestRun_OneTargetFails (0.00s)
  --- PASS: TestRun_ContextCancelled (0.00s)
  --- PASS: TestRun_NoTargets (0.00s)
  --- PASS: TestName (0.00s)
  --- PASS: TestRun_ScopeUnknownRepo (0.00s)
  --- PASS: TestNewService_DefaultClock (0.00s)
  --- PASS: TestRun_ScopeOneRepo (0.00s)
  --- PASS: TestRun_CancelledDuringSyncStillRecords (0.00s)
  --- PASS: TestRun_AllTargetsSucceed (0.00s)
  --- PASS: TestRun_UpsertFails (0.00s)
  --- PASS: TestRun_RecordFetchFails (0.00s)
      --- PASS: TestRun_RecordFetchFails/after_successful_sync (0.00s)
      --- PASS: TestRun_RecordFetchFails/after_failed_sync (0.00s)
  --- PASS: TestRun_RedactsCredentialsInSyncError (0.00s)
  --- PASS: TestRun_TokenNotLogged (0.00s)
      --- PASS: TestRun_TokenNotLogged/text (0.00s)
      --- PASS: TestRun_TokenNotLogged/json (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.200s
- `go test -race -coverprofile=<scratch>/cover1.out ./internal/ingest` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.239s	coverage: 100.0% of statements
- `git status --porcelain` -> exit 0
  ?? .claude/plans/m1/WO-05-ingest-service.report.md
  ?? internal/ingest/doc.go
  ?? internal/ingest/service.go
  ?? internal/ingest/service_test.go

Decisions:
- When Sync succeeds and only RecordFetch fails, the warn line is "target fetch not recorded" (slug, redacted error) instead of "target sync failed", because the sync itself worked. Upsert and sync failures still log "target sync failed".
- When ctx is done after the last target, Run returns `ingest stopped: <ctx error>` instead of the failure list, so a cancelled run always reports the cancellation (requirement 5). The failure list is not part of that error; each target's attempt is still in its RecordFetch row and its warn line.
- runError.Unwrap still returns the original, unredacted target errors so errors.Is works. Only text that ingest logs, stores or returns is redacted; a caller that formats an unwrapped error itself has to redact it.

Outside scope: the round-0 note about RecordFetch on a cancelled context is resolved by ING-002. The doc rows listed in round 0 still apply.
Concerns: none
Question or blocker: none

## Gate (lead, 2026-11-19)

make check rerun: exit 0; ingest tests ok. Astra round 0: REVISE (ING-001 high: unredacted sync error in log and repos row; ING-002 medium: RecordFetch on a cancelled context; ING-003 medium: counters lost when recording fails). The lead moved the redaction rules to model.Redact (e5de08a) for ING-001. Fix round 1 rechecked: make check exit 0; Astra round 1: APPROVED, all three addressed. Accepted.
