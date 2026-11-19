Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m0/WO-04-sqlite-store.md
Files changed:
- internal/store/db.go (created): DB, Open, Close, InTx, Bind, time and NULL helpers
- internal/store/migrate.go (created): Migrate, the unexported `migrate(ctx, fs.FS)` seam, loadMigrations, ErrSchemaTooNew, ErrMigrationChanged
- internal/store/runs.go (created): StartRun, FinishRun, LatestRuns
- internal/store/findings.go (created): UpsertFinding, ResolveFinding, ListOpenFindings
- internal/store/migrations/0001_initial_schema.sql (created)
- internal/store/helpers_test.go (created)
- internal/store/db_test.go (created)
- internal/store/runs_test.go (created)
- internal/store/findings_test.go (created)
- internal/store/interfaces_test.go (created): package store_test, `var _` assertions for pipeline.RunStore, pipeline.FindingStore, query.RunStore, query.FindingStore
- internal/store/doc.go: unchanged (package comment kept)

Commands run:
- `make check` -> exit 0
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.170s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/config	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/model	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/pipeline	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/sched	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	1.938s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/web	[no test files]
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
- `go test -race -count=1 ./internal/store/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	1.830s
  ```
- `go test -race -count=1 -run 'TestMigrate|TestSchema' -v ./internal/store/ | grep -c '^--- PASS'` -> `7` (expected at least 6)
- `go vet ./internal/store/` (with the four `var _` assertions in `interfaces_test.go`, package `store_test`) -> exit 0, no output
- `git status --porcelain` -> only owned files (this report file is added after this run)
  ```
  ?? internal/store/db.go
  ?? internal/store/db_test.go
  ?? internal/store/findings.go
  ?? internal/store/findings_test.go
  ?? internal/store/helpers_test.go
  ?? internal/store/interfaces_test.go
  ?? internal/store/migrate.go
  ?? internal/store/migrations/
  ?? internal/store/runs.go
  ?? internal/store/runs_test.go
  ```
- `go test -race -count=3 ./internal/store/` -> exit 0 (`ok ... 3.082s`), no flakes
- `go test -race -coverprofile=... ./internal/store/` -> `coverage: 85.8% of statements`
- `go mod tidy -diff` (read-only) -> exit 1 because there is a diff (see Outside scope); it writes nothing.

Tests (all pass): TestOpen_Pragmas (all 4 pooled connections), TestOpen_CreatesDirAndFileWithModes (dirs 0700; db, -wal and -shm 0600), TestOpen_ExistingFileKeepsData, TestOpen_RejectsURIPath (4 cases), TestMigrate_FreshAndIdempotent, TestMigrate_SchemaTooNew (exact text), TestMigrate_ChecksumChanged, TestMigrate_FailureLeavesNothing, TestLoadMigrations_RejectsBadNames, TestSchema_ForeignKeysEnforced (all 4 connections), TestSchema_AllDocTables, TestSchema_AcceptedAdditionsAndChecks, TestInTx_CommitRollbackPanic, TestInTx_Nested (3 cases), TestBind (2), TestTimestamps_RoundTrip (2), TestDB_ClosedReturnsErrors (8 methods), TestDB_StartFinishRun (3), TestDB_FinishRun_NotFound, TestDB_FinishRun_RejectsUnknownStatus, TestDB_LatestRuns (empty plus 3 jobs), TestDB_UpsertFinding_InsertThenUpdate, TestDB_UpsertFinding_StoreTimestamps, TestDB_UpsertFinding_JobFailureHasNullRefs, TestDB_UpsertFinding_Errors (3), TestDB_UpsertFinding_JoinsCallerTransaction, TestDB_ResolveFinding, TestDB_ListOpenFindings (empty, plus order and exclusions). No skips.

Decisions:
- `schema_migrations` columns: `version INTEGER PRIMARY KEY, name, checksum, applied_ts` (all TEXT NOT NULL). The template says `applied_at`; the doc rule "timestamp columns end in `_ts`" wins (see Outside scope).
- `ErrMigrationChanged` is exported, as in the template the WO says to follow exactly, even though the Produced list does not name it. Migrate wraps it with the file name.
- ErrSchemaTooNew text is exactly `database schema is newer than this binary: database at version N, binary knows M`, with no `migrate:` prefix, to match the WO example. N is the highest applied version, so the message does not depend on map iteration order.
- Open creates missing parent directories with `os.MkdirAll(dir, 0700)` and the file with `O_CREATE|O_EXCL, 0600` before the driver opens it. An existing file or directory keeps its mode. SQLite then creates -wal and -shm with the database file's mode (0600), and a test checks this.
- Open joins the `pool.Close()` error into its error on the failure paths; the template discards it.
- Nullability rule for tables this WO does not write. NOT NULL applies to: primary-key and doc-stated UNIQUE columns; the foreign key that ties a child row to its parent (commits/manifests.repo_id; dep_events repo/commit/manifest/package; dep_versions repo/package/manifest; symbol_usage repo/package; package_upstream both; advisory_affected both; advisory_matches repo/dep_version/advisory; upstream_issues.upstream_repo_id; comments.issue_id; issue_candidates issue/repo/package; llm_verdicts issue/repo/package; eval_labels issue/repo/package); `created_ts`/`updated_ts`; and the doc defaults (`upstream_repos.watch_issues NOT NULL DEFAULT 1`, `advisories.kev NOT NULL DEFAULT 0`). Every other column is nullable with no default, including `dep_versions.from_commit_id`, `issue_candidates.dep_version_id`, `llm_verdicts.evidence_verified` and `digests.error`.
- Types: `*_id` INTEGER except `advisory_id` (TEXT, references `advisories.id`); `*_ts` TEXT; flags and counts INTEGER (`direct`, `is_pull`, `passed`, `kev`, `watch_issues`, `evidence_verified`, `number`, `position`, `line`, `count`, `comments_count`, tokens, `latency_ms`, `last_poll_status`, `http_cache.status`); `score`, `confidence` (except `advisory_matches.confidence`, which is TEXT high/medium/low), `cvss_score`, `epss*` and `cost_usd` REAL; JSON columns TEXT; `settings.value` BLOB (the doc ER shows blob); everything else TEXT. `kv.value` and `settings.value` are NOT NULL.
- runs: `job`, `started_ts`, `status` (CHECK), `stats` and `error` are NOT NULL. StartRun stores `stats='{}'` and `error=''`. An empty stored stats object reads back as nil Stats, so nil and empty Stats both round-trip as nil. StartRun returns StartedAt as stored (UTC, truncated to the second).
- findings: `kind` (CHECK), `fingerprint` (UNIQUE), `score`, `rank_reason`, `first_seen_ts` and `last_changed_ts` are NOT NULL, so a zero `LastChangedAt` fails the upsert with the constraint error. The six reference columns are nullable; zero IDs and an empty AdvisoryID are stored as NULL.
- The initial `finding_state` row stores only `state='open'` plus `created_ts`/`updated_ts`. `snooze_until_ts`, `note`, `changed_ts` and `changed_by` stay NULL because the operator has not changed anything yet, so the returned State has a zero ChangedAt.
- UpsertFinding runs, inside `InTx` (and so joins a caller's transaction): `INSERT ... ON CONFLICT(fingerprint) DO UPDATE ... RETURNING id`, then `INSERT INTO finding_state ... ON CONFLICT(finding_id) DO NOTHING`, then reads the row back joined with its state. It also bumps `updated_ts` on update.
- ResolveFinding also sets `updated_ts`.
- LatestRuns: `WHERE id IN (SELECT MAX(id) FROM runs GROUP BY job) ORDER BY job`, served by the `runs(job, id)` index.
- One column constant and scan helper per query shape: `findingColumns` reads findings joined with finding_state, because every finding the store returns carries its State.
- dep_state view: an explicit column list (`id … to_ts, created_ts, updated_ts`) rather than `SELECT *`.
- Index names follow `<table>_<column>`. Foreign-key child indexes are skipped where the column already leads a UNIQUE or PRIMARY KEY index (commits/manifests.repo_id, dep_events.commit_id, package_upstream.package_id, upstream_issues.upstream_repo_id, comments.issue_id, issue_candidates.issue_id, eval_labels.issue_id, advisory_matches.dep_version_id, finding_state.finding_id).
- Test setup writes parent rows (repos, packages, advisories) and non-open states with raw SQL, because no store method writes them yet.

Outside scope:
- `.claude/skills/sqlite-store/templates/open-and-migrate.md` names the column `applied_at`, which breaks the `_ts` rule in `docs/database.md`; the code uses `applied_ts`. The template also discards `pool.Close()` errors in Open, which `go-code` forbids.
- `go.mod` (lead-owned): `modernc.org/sqlite v1.62.0` is still marked `// indirect` although `internal/store` now imports it directly. `go mod tidy -diff` would make it a direct require and would also drop `robfig/cron/v3` and `go.yaml.in/yaml/v3`, which nothing imports yet. The build and `make check` are unaffected. Not touched.
- Doc changes for docs-sync (`docs/database.md`):
  - list `schema_migrations` columns (`version`, `name`, `checksum`, `applied_ts`);
  - "Every table has `created_ts` and `updated_ts`" is not true for `kv`, `http_cache`, `settings` (only `updated_ts`) or `schema_migrations` (neither);
  - record the nullability and type choices above, the CHECK constraints on `runs.status`, `findings.kind` and `finding_state.state`, and `settings.value` as BLOB;
  - `runs.stats` is always a JSON object (`{}` when empty) and `runs.error` is `''` when there is no failure;
  - the initial `finding_state` row leaves `changed_ts` and `changed_by` NULL.
  `docs/data-access.md` needs no change for this WO.

Concerns:
- Foreign keys use SQLite's default `ON DELETE NO ACTION` (plain `REFERENCES`, as the WO says). The sqlite-store skill describes "Deleting and rebuilding a repo's `dep_versions`" as a normal timeline write. With these constraints that delete fails while any `advisory_matches`, `issue_candidates` or `findings` row points at those intervals. The same applies to rebuilding `commits` rows that `dep_versions.from_commit_id`/`to_commit_id` reference. Under the additive-only rule an FK action cannot be changed after merge (it needs a table rebuild), so this is the moment to decide whether some references should be `ON DELETE CASCADE` or `SET NULL`, or whether the timeline must rebuild without deleting (diff and update in place). I did not pick an action because it changes behaviour that M1 and M2 code will see.
- With `foreign_keys=1`, a finding whose kind is `advisory` or `issue` cannot be written until its repo, package, advisory, issue or verdict rows exist. That is intended, but callers must save parents first, in the same transaction or earlier.

## Fix round 1

Status: DONE
Work order: .claude/plans/m0/WO-04-sqlite-store.md (fix round 1 ruling, quoted in the coordinator's message)
Files changed:
- internal/store/migrations/0001_initial_schema.sql (modified): `advisory_matches.dep_version_id` and `issue_candidates.dep_version_id` are now `REFERENCES dep_versions (id) ON DELETE CASCADE`; `findings.dep_version_id` is now `REFERENCES dep_versions (id) ON DELETE SET NULL`; the header comment states the delete rule. Every other foreign key is unchanged (`NO ACTION`).
- internal/store/db_test.go (modified): added `TestSchema_DepVersionDeleteBehaviour`.
Commands run:
- `go test -race -count=1 -run TestSchema_DepVersionDeleteBehaviour -v ./internal/store/` -> exit 0
  ```
  === RUN   TestSchema_DepVersionDeleteBehaviour
  === PAUSE TestSchema_DepVersionDeleteBehaviour
  === CONT  TestSchema_DepVersionDeleteBehaviour
  --- PASS: TestSchema_DepVersionDeleteBehaviour (0.07s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	1.393s
  ```
- `make check` -> exit 0
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/config	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/model	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/pipeline	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/sched	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	1.835s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/web	[no test files]
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
- `go test -race -count=1 ./internal/store/` -> exit 0 (`ok ... 1.827s`)
- `go test -race -count=1 -run 'TestMigrate|TestSchema' -v ./internal/store/ | grep -c '^--- PASS'` -> `8` (expected at least 6)
- `go vet ./internal/store/` -> exit 0
- `git status --porcelain` -> only the files under internal/store/ and this report file
Decisions:
- The test seeds repo, package, manifest, commit, interval, advisory, advisory match, upstream repo, issue and issue candidate with raw SQL, and writes the finding through `UpsertFinding` with `DepVersionID` set. After `DELETE FROM dep_versions` it checks: no advisory_matches rows, no issue_candidates rows, the finding still present with `dep_version_id IS NULL`, and its finding_state row still present. It then inserts a second interval on the same commit and checks that deleting the commit fails. Under the old `NO ACTION` schema the test fails at the interval delete.
Outside scope: none new. The go.mod item from the first round is left to the lead, as instructed.
