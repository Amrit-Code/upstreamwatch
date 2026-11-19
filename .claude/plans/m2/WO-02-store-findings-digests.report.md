Status: DONE
Work order: .claude/plans/m2/WO-02-store-findings-digests.md
Files changed:
- internal/store/db.go (modified)
- internal/store/db_test.go (modified)
- internal/store/findings.go (modified)
- internal/store/findings_test.go (modified)
- internal/store/runs.go (modified)
- internal/store/repos.go (modified)
- internal/store/httpcache.go (modified)
- internal/store/digests.go (created)
- internal/store/digests_test.go (created)
- internal/store/backup.go (created)
- internal/store/backup_test.go (created)
- internal/store/writequeue_test.go (created)
- .claude/plans/m2/WO-02-store-findings-digests.report.md (created)

Commands run:
- `make check` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/findings	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	2.100s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ops	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.907s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.617s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.628s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	5.719s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.910s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/vuln	[no test files]
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
- `go test -race -count=1 -run 'TestUpsertFinding|TestListFindings|TestSaveFindingState|TestGetFinding|TestMarkReported|TestDigests|TestBackupTo|TestWriteQueue' ./internal/store/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.303s
- `go test -race -count=1 ./internal/pipeline/ ./internal/query/ ./internal/web/ ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.443s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.347s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.243s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.744s
- `go test -race -count=10 -run 'TestWriteQueue|TestDB_ConcurrentWriters|TestDB_InTx' ./internal/store/` -> exit 0 (flake check)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	12.626s
- `go test -race -count=1 -coverprofile=... ./internal/store/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.457s	coverage: 87.0% of statements
  (BackupTo 77.8%, write/enterWriteQueue/leaveWriteQueue 100%, every new findings and digests method 84.6% to 100%)
- Mutation check: with `write` made to skip the queue (`if true || db.inTx`), `go test -race -count=1 -run 'TestWriteQueue_SingleWrite' -v ./internal/store/` -> FAIL, as intended:
  writequeue_test.go:97: queued InTx error = start run "late": context deadline exceeded, want it to come from the write queue wait
  writequeue_test.go:69: StartRun() returned start run "queued": database is locked (5) (SQLITE_BUSY) while the transaction was open, want it to wait
  The mutation was reverted before the runs above.
- Scratch compile check (file created, vetted, deleted; not in the diff): `var _ vuln.FindingStore`, `pipeline.FindingStore`, `findings.Store`, `ops.Backuper = (*store.DB)(nil)`; `go vet ./internal/store/` -> exit 0
- `git status --porcelain` -> only owned files (before this report was written):
   M internal/store/db.go
   M internal/store/db_test.go
   M internal/store/findings.go
   M internal/store/findings_test.go
   M internal/store/httpcache.go
   M internal/store/repos.go
   M internal/store/runs.go
  ?? internal/store/backup.go
  ?? internal/store/backup_test.go
  ?? internal/store/digests.go
  ?? internal/store/digests_test.go
  ?? internal/store/writequeue_test.go

Decisions:
- Queue helper: `(*DB).write(ctx, fn func() error) error` in db.go, sharing `enterWriteQueue`/`leaveWriteQueue` with `InTx`. The wait error reads "wait for write queue: <ctx err>"; InTx keeps its existing "begin transaction: wait for write queue: ..." text. UpsertFinding and MarkReported go through InTx; every other write goes through `write`.
- Busy-timeout test hook: Open now calls an unexported `open(ctx, path, busy, logger)`, and `dsnParams` became a function of the timeout. Open still passes 5 s, so production behaviour is unchanged.
- UpsertFinding's last-changed rule is a CASE inside the existing `ON CONFLICT DO UPDATE`. SQLite evaluates every right-hand side against the row as it was before the update, so no extra read is needed. `updated_ts` still moves on every upsert, because the row is written (dep_version relink).
- ListFindings returns each finding's stored state. The effective state is used only for filtering, so callers apply `FindingState.Effective` (WO-05) themselves. A snoozed row with NULL `snooze_until_ts` counts as expired, which matches the zero-time reading of `Effective`.
- ListFindings binds `Suppressed` as 0/1 and repeats the RepoID and Kind arguments instead of using numbered placeholders.
- MarkReported writes its 500-ID chunks in one InTx, so they land all or nothing and join a caller's transaction (digest row plus marks). IDs with no finding are skipped silently.
- SaveDigest stores an empty payload as '' (the column stays non-NULL) and reads '' or NULL back as nil. It returns the record with ID set and SentAt normalised to UTC whole seconds, as StartRun does.
- MarkDigestRetried updates a row whatever its current status. The work order states no precondition.
- BackupTo creates the target first with O_CREATE|O_EXCL and mode 0600, then runs `VACUUM INTO ?`, which accepts an empty file (checked against v1.62.0 by TestBackupTo), then chmods to 0600. This refuses an existing path with no check-then-write race. A VACUUM failure removes the file it created. It also refuses "", paths containing '?' and paths starting with "file:", as Open does, because SQLite would read those as URIs and write somewhere other than the file it created.
- Tests added beyond the listed ones: TestListUnresolvedFindings, TestGetFinding_Found, TestWriteQueue_EveryWriteWaits (each write method with a cancelled context while the queue is held must fail in the queue wait), TestWriteQueue_WritesInsideTxDoNotWaitAgain (every write inside one InTx, under synctest, so a self-deadlock shows up as an error), plus new methods in TestDB_ClosedReturnsErrors.
- TestWriteQueue_SingleWriteWaitsForLongTx uses a 200 ms busy timeout and a `select` on `time.After(3*busy)`, not synctest, because SQLite's busy timeout runs on the real clock. A control write that bypasses the queue confirms that the shortened timeout gives SQLITE_BUSY. The subtest takes about 0.9 s.

Outside scope:
- internal/pipeline/interfaces.go: the `FindingStore.UpsertFinding` doc comment still says an existing fingerprint "updates score, rank reason and last-changed time". Under requirement 2, last-changed now moves only when score or rank reason changes or the finding was resolved. internal/vuln/interfaces.go already defers to the store's comment. Lead to update.
- internal/store/migrate.go:52: `migrate` runs `CREATE TABLE IF NOT EXISTS schema_migrations` on the pool outside the write queue. It runs at startup before any other writer, so the risk is low, but it is the one remaining write that skips the queue. The file is not owned here.
- internal/store/interfaces_test.go (not owned) asserts only the M1 interfaces. It could gain `vuln.FindingStore`, `findings.Store`, `ops.Backuper`, and `report.Store` once WO-01's GetFindingDetail lands. The first three compile today (scratch check above).
- docs/database.md "digests" says `error` is "otherwise empty", while the work order stores NULL when empty. Both read back as "" through the model, so this was not treated as a contradiction. docs-sync may want "NULL when the send succeeded".
- Docs to sync: docs/data-access.md "What is built" (FindingStore gains ListFindings, ListUnresolvedFindings, GetFinding, SaveFindingState and MarkReported; a digests row with SaveDigest, FailedDigests, MarkDigestRetried and LastSentDigest; BackupTo; the new UpsertFinding last-changed rule), and the Rules bullet on the write queue (single-statement writes now wait in it too).

Concerns: none
Question or blocker: none
