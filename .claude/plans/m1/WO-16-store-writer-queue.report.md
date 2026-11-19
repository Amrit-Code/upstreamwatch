Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-16-store-writer-queue.md
Files changed:
- internal/store/db.go (modified: `DB.writeQ` field, created in `Open`, acquired and released in `InTx`)
- internal/store/db_test.go (modified: tests added; no existing test changed)
- .claude/plans/m1/WO-16-store-writer-queue.report.md (created)

Commands run:
- `git log --oneline -1` -> exit 0
  b417cd2 plans: WO-16 scope of requirement 4
- `go test -race -count=1 -run 'TestDB_InTx|TestInTx|TestDB_ConcurrentWriters' -v ./internal/store/` -> exit 0
  --- PASS: TestDB_InTx_ContextCancelledWhileQueued (0.00s)
      --- PASS: TestDB_InTx_ContextCancelledWhileQueued/cancelled_after_queueing (0.22s)
      --- PASS: TestDB_InTx_ContextCancelledWhileQueued/deadline_exceeded (0.22s)
      --- PASS: TestDB_InTx_ContextCancelledWhileQueued/already_cancelled_while_queue_is_held (0.22s)
  --- PASS: TestDB_InTx_QueueReleasedAfterErrorAndPanic (0.23s)
  --- PASS: TestInTx_CommitRollbackPanic (0.00s)
      --- PASS: TestInTx_CommitRollbackPanic/rollback_on_error (0.23s)
      --- PASS: TestInTx_CommitRollbackPanic/rollback_on_panic (0.23s)
      --- PASS: TestInTx_CommitRollbackPanic/commit (0.23s)
  --- PASS: TestDB_InTx_QueueServesArrivalOrder (0.24s)
  --- PASS: TestInTx_Nested (0.00s)
      --- PASS: TestInTx_Nested/outer_error_rolls_back_inner_writes (0.23s)
      --- PASS: TestInTx_Nested/inner_error_rolls_back_everything (0.07s)
      --- PASS: TestInTx_Nested/inner_joins_outer_and_commits_with_it (0.07s)
  --- PASS: TestDB_ConcurrentWriters (0.68s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.015s
- `go test -race -count=20 -run 'TestDB_ConcurrentWriters|TestDB_InTx|TestInTx' ./internal/store/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	14.655s
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.082s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.486s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.340s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.387s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	8.618s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.767s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.813s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.246s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.174s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.546s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.456s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.411s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.482s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.260s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.239s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.227s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.515s
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
- `git status --porcelain` -> exit 0 (run before this report file was written)
   M internal/store/db.go
   M internal/store/db_test.go

Decisions:
- Queue mechanism: an unexported `writeQ chan struct{}` with a buffer of one on `DB`, made in `Open`. `InTx` copies `*db`, so every derived `*DB` shares the channel. Acquire is a `select` on sending to the channel or `<-ctx.Done()`. Release is a deferred receive. The comment on the field gives the fairness reasoning: parked senders wait in the channel's FIFO send queue, and a receive moves the head waiter's value into the buffer under the channel lock, so a later arrival cannot overtake a waiter.
- Ordering of releases: the release defer is registered before the existing rollback/recover defer, so it runs after commit, after rollback and after the rollback on panic, while the panic keeps unwinding.
- Error on giving up the wait: `fmt.Errorf("begin transaction: wait for write queue: %w", ctx.Err())`. It reuses the existing "begin transaction" prefix. `errors.Is(err, context.Canceled)` and `errors.Is(err, context.DeadlineExceeded)` both hold.
- Context already done and queue free: `select` may take either case. If it takes the queue, `BeginTx(ctx)` fails with the context error, which is still wrapped and still opens no transaction. I added no separate pre-check.
- Tests: `TestDB_InTx_ContextCancelledWhileQueued` has three subtests: cancelled after parking (uses `synctest.Wait` to be sure the second InTx is queued, then releases the holder, checks the first committed and the second wrote nothing, then checks a new InTx succeeds); already cancelled while the queue is held; deadline exceeded (synctest fake clock). Also added `TestDB_InTx_QueueServesArrivalOrder` (five waiters queued one at a time under synctest are served in arrival order) and `TestDB_InTx_QueueReleasedAfterErrorAndPanic`. `TestDB_ConcurrentWriters` and `TestInTx_Nested` are unchanged.

Outside scope:
- Requirement 4, single-statement writes outside `InTx` when called on the root `*DB`. These are not queued and still rely on busy_timeout if they meet an open InTx in the same process. I left them unchanged:
  - `StartRun` (internal/store/runs.go:66, INSERT ... RETURNING)
  - `FinishRun` (internal/store/runs.go:86, UPDATE)
  - `UpsertRepo` (internal/store/repos.go:40, INSERT ... ON CONFLICT ... RETURNING)
  - `RecordFetch` (internal/store/repos.go:60, UPDATE)
  - `SaveHTTPValidator` (internal/store/httpcache.go:61, upsert)
  - `ResolveFinding` (internal/store/findings.go:129, UPDATE)
  - `Migrate`'s `CREATE TABLE IF NOT EXISTS schema_migrations` on the pool (internal/store/migrate.go:52)
- These multi-statement writers already go through `InTx` and are now queued: `UpsertFinding` (findings.go:79), `ApplyTimeline` (timeline.go:186), and each migration in `migrate` (migrate.go:84).
- Doc row for docs-sync: the `InTx` sentence in docs/data-access.md "Rules" could add that in-process write transactions queue in arrival order, honour the caller's context, and leave busy_timeout to cover other processes.

Concerns:
- Different failure mode for one misuse. If code inside `fn` calls `InTx` on the outer `*DB` instead of the `tx` it was given, that call used to fail after 5 s with SQLITE_BUSY. It now waits on the queue until its context ends, which can be forever with a background context. The sqlite-store skill already forbids this use, and grep finds no `InTx` or `store.Bind` caller outside internal/store yet. I documented it in the `InTx` doc comment. A reviewer may want to confirm that this is acceptable or ask for a guard; a guard would be outside this work order.
- The ordering and cancel-while-parked subtests open SQLite inside a `synctest` bubble. They passed 20 times under `-race`. If a future driver version starts a background goroutine with a real timer, they could stall rather than fail cleanly.

Question or blocker: none

## Fix round 1

Status: DONE
Work order: .claude/plans/m1/WO-16-store-writer-queue.md
Files changed:
- internal/store/db_test.go (modified: new helper `checkQueueWaitErr`, used by all three `TestDB_InTx_ContextCancelledWhileQueued` subtests; the already-cancelled subtest also gets an elapsed-time bound)
- internal/store/db.go (unchanged this round; its queue-wait error already reads `begin transaction: wait for write queue: <ctx error>`)

Commands run:
- Mutation check. I deleted the acquire `select` and the release defer from `InTx` with Edit (backup at the scratchpad path db.go.orig), then ran `go test -race -count=1 -run 'TestDB_InTx_ContextCancelledWhileQueued|TestDB_InTx_QueueServesArrivalOrder' ./internal/store/` -> FAIL
  --- FAIL: TestDB_InTx_ContextCancelledWhileQueued (0.00s)
      --- FAIL: TestDB_InTx_ContextCancelledWhileQueued/already_cancelled_while_queue_is_held (0.28s)
          db_test.go:726: queued InTx error = begin transaction: context canceled, want it to come from the write queue wait
      --- FAIL: TestDB_InTx_ContextCancelledWhileQueued/deadline_exceeded (0.42s)
          db_test.go:748: queued InTx error = begin transaction: context deadline exceeded, want it to come from the write queue wait
      --- FAIL: TestDB_InTx_ContextCancelledWhileQueued/cancelled_after_queueing (0.46s)
          db_test.go:681: queued InTx error = begin transaction: context canceled, want it to come from the write queue wait
  --- FAIL: TestDB_InTx_QueueServesArrivalOrder (0.47s)
      db_test.go:795: waiters served in order [4 3 0 1 2], want [0 1 2 3 4]
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/store	0.748s
- I restored db.go from the backup. `grep -n 'wait for write queue' internal/store/db.go` printed line 137, and `git diff --stat` shows db.go at its round-0 size (32 lines changed).
- `go test -race -count=20 -run 'TestDB_ConcurrentWriters|TestDB_InTx|TestInTx' ./internal/store/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	15.551s
- `make check` -> exit 0
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.383s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- `git status --porcelain` -> exit 0
   M internal/store/db.go
   M internal/store/db_test.go
  ?? .claude/plans/m1/WO-16-store-writer-queue.report.md

Decisions:
- I chose the error-text check plus a timing bound and did not add a BeginTx hook or counter, because a hook would put test-only code into db.go. The text check proves the queue: `InTx` produces "wait for write queue" only in the queue-wait branch, and that branch returns before `BeginTx`. A caller that skipped the queue fails with `begin transaction: context canceled`, which the mutation run showed.
- The already-cancelled subtest runs on the real clock and also asserts the call returns in under 1 s, well inside the 5 s busy_timeout. The other two subtests run under synctest, where the clock is fake and a timing bound would prove nothing, so they rely on the text check and the `ran` flag.

Outside scope: none new.
Concerns: none new. The round-0 concerns still stand: a nested call on the outer `*DB` now deadlocks until its context ends instead of failing after 5 s, and the synctest bubble tests depend on the driver starting no timer goroutines.
Question or blocker: none

## Gate (lead, 2026-11-19)

make check rerun: exit 0; `go test -race -count=20 -run 'TestDB_ConcurrentWriters|TestDB_InTx|TestInTx' ./internal/store/` ok. Sonnet reviewer: ACCEPT (release on every path, nesting, FIFO fairness); minor: cancel-while-queued test passed without the queue, fixed in round 1 (asserts the queue-wait error and timing; agent showed it fails with the acquire removed). Self-deadlock on misuse accepted (documented, forbidden by the skill, no caller). Unqueued single-statement writes carried to the M2 checklist. Accepted.
