# WO-02: Store methods for finding state, digests and backup; every write through the queue

Milestone: M2
Agent: go-builder   Model: opus
Depends on: none (interface files committed)
Report file: `.claude/plans/m2/WO-02-store-findings-digests.report.md`
Ticket: UPW-49, UPW-53, UPW-56 (store halves), plus the M1 carry-over on the write queue

## Objective

`*store.DB` implements the finding reads and writes that the vulns job, the digest job, `findings.Service` and the web pages need; the `digests` table methods; `BackupTo` with `VACUUM INTO`; and every existing single-statement write goes through the in-process write queue. Serves M2 checklist items 13 to 17 and 19, and the carried M1 note at the end of the M2 checklist.

## Owned files

- `internal/store/findings.go`, `internal/store/findings_test.go`
- `internal/store/digests.go`, `internal/store/digests_test.go` (new)
- `internal/store/backup.go`, `internal/store/backup_test.go` (new)
- `internal/store/db.go`, `internal/store/db_test.go`
- `internal/store/runs.go`, `internal/store/repos.go`, `internal/store/httpcache.go` (only to route their writes through the queue)
- `internal/store/writequeue_test.go` (new)

Read-only for you: every other file in `internal/store/` (WO-01 creates `advisories.go`, `kv.go`, `finding_detail.go` in parallel and calls `scanFinding`, `formatTime`, `parseTime`, `nullID`, `nullText`; keep those helpers' names and signatures), `internal/store/migrations/`, `internal/model/*.go`, every `interfaces.go`, `go.mod`, `go.sum`. Tests in other packages (`internal/pipeline`, `internal/query`, `internal/web`, `cmd/upw`) must keep passing; if one fails because of a contract change listed here, report it instead of editing it.

## Interfaces

### Consumed

`internal/model/finding.go`, `internal/model/digest.go`: read in full.

### Produced (methods on `*store.DB`)

```go
// internal/vuln/interfaces.go FindingStore, internal/pipeline/interfaces.go FindingStore
UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error)            // changed semantics, requirement 2
ResolveFinding(ctx context.Context, fingerprint string, at time.Time) error            // unchanged, now queued
ListUnresolvedFindings(ctx context.Context, kind model.FindingKind) ([]model.Finding, error)

// internal/report/interfaces.go Store (all but GetFindingDetail, which WO-01 writes)
ListFindings(ctx context.Context, f model.FindingFilter) ([]model.Finding, error)
MarkReported(ctx context.Context, ids []int64, at time.Time) error
SaveDigest(ctx context.Context, rec model.DigestRecord) (model.DigestRecord, error)
FailedDigests(ctx context.Context) ([]model.DigestRecord, error)
MarkDigestRetried(ctx context.Context, id int64) error
LastSentDigest(ctx context.Context) (model.DigestRecord, error)

// internal/findings/interfaces.go Store
GetFinding(ctx context.Context, id int64) (model.Finding, error)
SaveFindingState(ctx context.Context, id int64, st model.FindingState) error

// internal/ops/interfaces.go Backuper
BackupTo(ctx context.Context, path string) error
```

## Requirements

1. Every write method on `*store.DB`, new and existing (`StartRun`, `FinishRun`, `UpsertRepo`, `RecordFetch`, `SaveHTTPValidator`, `ResolveFinding`, and every new write above), takes the in-process write queue before it writes, either by running in `InTx` or through a small helper in `db.go` that waits on `db.writeQ` (honouring the caller's context exactly as `InTx` does) and runs one statement. Called on a `*DB` already inside a transaction, it runs directly (no second wait).
2. `UpsertFinding` changes for an existing fingerprint: it still relinks `dep_version_id`, writes score and rank reason, and clears `resolved_ts`; but `last_changed_ts` moves to `f.LastChangedAt` only when the stored score differs from `f.Score`, the stored rank reason differs from `f.RankReason`, or the finding was resolved. Otherwise `last_changed_ts` is unchanged. A new finding still gets first-seen and last-changed from `f.LastChangedAt`. Update the method's doc comment and the copy of the contract in `internal/pipeline/interfaces.go` is the lead's to update (report it in Outside scope).
3. `ListFindings(f)` returns unresolved findings only, highest score first, then lowest ID. The effective state is the stored state except that a `snoozed` finding whose `snooze_until_ts` is at or before `f.Now` counts as `open`. With `Suppressed` false it returns findings whose effective state is `open` or `acked`; with `Suppressed` true it returns all unresolved findings. `RepoID` and `Kind` filter when set. A zero `f.Now` is an error.
4. `ListUnresolvedFindings(kind)`: unresolved findings of that kind in any state, ordered by ID.
5. `GetFinding(id)`: the finding with its state; unknown ID → error wrapping `model.ErrNotFound`.
6. `SaveFindingState(id, st)`: updates `state`, `snooze_until_ts` (NULL when zero), `note` (NULL when empty), `changed_ts` (NULL when zero), `changed_by` (NULL when empty) and `updated_ts`; unknown ID → error wrapping `model.ErrNotFound`. It does not validate transitions (the model does).
7. `MarkReported(ids, at)`: sets `reported_in_digest_ts` and `updated_ts` on those rows only; `last_changed_ts` unchanged; an empty slice is a no-op; chunk over 500 IDs.
8. Digests: `SaveDigest` inserts `channel`, `status`, `sent_ts` (from `SentAt`), `payload` (text), `error` (NULL when empty) and returns the record with its ID. `FailedDigests` returns rows with status `failed` ordered by ID. `MarkDigestRetried` sets status `retried`; unknown ID → `model.ErrNotFound`. `LastSentDigest` returns the row with status `sent` and the greatest `sent_ts` (then ID), or `model.ErrNotFound`.
9. `BackupTo(path)`: refuses (error, nothing written) when `path` exists; creates the parent directory with mode 0700 if missing; runs `VACUUM INTO ?` with the path as a bound parameter; sets the file's mode to 0600. It does not need the write queue (it only reads) but must not run inside a transaction (error if called on a tx `*DB`).
10. `ListOpenFindings` keeps its current behaviour (it is still used until the web work order switches to `ListFindings`).

## Tests to include

- `TestUpsertFinding_LastChangedOnlyOnChange`: insert at t1; upsert same score and reason at t2 → last-changed t1; upsert new score at t3 → t3; resolve at t4, upsert same score and reason at t5 → t5 and resolved cleared.
- `TestListFindings_EffectiveState`: findings in open, acked, snoozed (until future), snoozed (until past), dismissed, plus one resolved → `Suppressed=false` gives open, acked and the expired snooze, ordered by score; `Suppressed=true` gives all five unresolved; `RepoID` and `Kind` filters; zero `Now` → error.
- `TestSaveFindingState_RoundTrip`, `TestGetFinding_NotFound`, `TestMarkReported`.
- `TestDigests`: save sent and failed rows on two channels → `FailedDigests`, `MarkDigestRetried`, `LastSentDigest`; empty table → `ErrNotFound`.
- `TestBackupTo`: back up a database with rows → file exists with mode 0600, opens with `store.Open`, has the rows; existing path → error.
- `TestWriteQueue_SingleWriteWaitsForLongTx`: hold an `InTx` open longer than the busy timeout would allow for a competing writer in the same process (shorten the timeout through an unexported test hook if needed so the test stays under a few seconds), meanwhile call `StartRun` from another goroutine → it waits and succeeds after the transaction commits, with no `SQLITE_BUSY`. One more case: `StartRun` with a context that ends while waiting → returns an error wrapping `context.DeadlineExceeded` and writes nothing.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'TestUpsertFinding\|TestListFindings\|TestSaveFindingState\|TestGetFinding\|TestMarkReported\|TestDigests\|TestBackupTo\|TestWriteQueue' ./internal/store/` | `ok` |
| `go test -race ./internal/pipeline/ ./internal/query/ ./internal/web/ ./cmd/upw/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Advisory tables, kv, `GetFindingDetail` (WO-01).
- Transition rules and snooze validation (WO-05, in `internal/model`).
- `findings.Service`, the digest job, the backup job and retention (later work orders).

## Skills and docs

Load with the Skill tool: `sqlite-store`.
Read: `docs/data-access.md` (Rules), `docs/database.md` (Findings, Ops), `.claude/plans/m1/WO-16-store-writer-queue.md` for how the queue was built.

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings

