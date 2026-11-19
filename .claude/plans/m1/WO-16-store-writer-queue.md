# WO-16: Queue in-process write transactions in the store

Milestone: M1
Agent: go-builder   Model: opus
Depends on: everything through WO-14 (merged)
Report file: `.claude/plans/m1/WO-16-store-writer-queue.report.md`
Ticket: UPW-40 (found while checking CI for the M1 security fixes)

## Objective

`TestDB_ConcurrentWriters` failed once in CI (`concurrent InTx error = begin transaction: database is locked (5) (SQLITE_BUSY)`, after 6.33 s on a 2-core runner under `-race`). Several goroutines of one process that open `IMMEDIATE` transactions at once rely on SQLite's busy handler, which is not fair, so one writer can wait longer than the 5-second `busy_timeout` and fail. In production the scheduler's jobs and the web server's state writes share one process, so the same starvation can happen under load. After this work order, write transactions from one process wait their turn in Go, fairly and honouring their context, and SQLite's busy timeout is left to cover other processes (an `upw run` beside `upw serve`).

## Owned files

- `internal/store/db.go` (the `InTx` path and the `DB` struct only)
- `internal/store/*_test.go` (add tests; do not weaken `TestDB_ConcurrentWriters`)

Read-only: migrations, every other store file, every other package.

## Requirements

1. All `*DB` values derived from one `store.Open` share one write queue. `InTx` (the outermost call; a nested `InTx` joins the transaction as today) acquires the queue before `BEGIN IMMEDIATE` and releases it after commit or rollback, including on panic.
2. Acquiring the queue honours the caller's context: if the context ends while waiting, `InTx` returns an error wrapping the context's error and opens no transaction.
3. Waiters are served in arrival order closely enough that no writer waits behind an unbounded number of later arrivals (a buffered channel of size 1, or a `sync.Mutex` plus context handling, is acceptable if the reasoning is written in a comment; Go's `sync.Mutex` has starvation mode).
4. Reads outside `InTx` are not queued. Single-statement writes outside `InTx` (if any exist) are listed in the report for the lead; do not change them (they are outside the owned files).
5. `busy_timeout` stays at 5000 ms for cross-process contention.

## Tests to include

- `TestDB_ConcurrentWriters` unchanged and passing with `-race -count=20`.
- `TestDB_InTx_ContextCancelledWhileQueued`: hold the queue in one `InTx` (blocked on a channel), start a second `InTx` with a context that is cancelled → it returns an error wrapping `context.Canceled` without running its function; releasing the first lets it commit.
- `TestInTx_Nested` (existing, in db_test.go) still passes (nested calls must not deadlock on the queue).

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=20 -run 'TestDB_ConcurrentWriters\|TestDB_InTx\|TestInTx' ./internal/store/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `sqlite-store`, `go-test`.
Read: `internal/store/db.go`, `docs/data-access.md` ("Rules").

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.
