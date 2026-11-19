# WO-04: SQLite store core, initial schema, runs and findings methods

Milestone: M0
Agent: go-builder   Model: opus
Depends on: WO-01, WO-02 (merged)
Report file: `.claude/plans/m0/WO-04-sqlite-store.report.md`
Ticket: UPW-14

## Objective

`internal/store` opens the SQLite file with the required pragmas, applies embedded forward-only migrations tracked in `schema_migrations`, refuses a database newer than the binary, offers `InTx` and `Bind`, and implements the run and finding methods that `pipeline` and `query` declare. Serves M0 checklist items 8 to 11.

## Owned files

- `internal/store/` (all files, including `migrations/0001_initial_schema.sql` and tests; keep the package comment in `doc.go`)

Lead-owned, read-only for you (stop and report if one is wrong):

- `internal/model/` (all files)
- `internal/pipeline/interfaces.go`, `internal/query/interfaces.go`
- `go.mod`, `go.sum` (`modernc.org/sqlite v1.62.0` is already required)
- `.go-arch-lint.yml`, `Makefile`, `scripts/`

## Interfaces

### Consumed

The model types in `internal/model/run.go`, `internal/model/finding.go`, `internal/model/errors.go`, and the method sets you must satisfy:

```go
// internal/pipeline/interfaces.go
type RunStore interface {
	StartRun(ctx context.Context, job string, startedAt time.Time) (model.Run, error)
	FinishRun(ctx context.Context, run model.Run) error
}
type FindingStore interface {
	UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error)
	ResolveFinding(ctx context.Context, fingerprint string, at time.Time) error
}

// internal/query/interfaces.go
type RunStore interface {
	LatestRuns(ctx context.Context) ([]model.Run, error)
}
type FindingStore interface {
	ListOpenFindings(ctx context.Context) ([]model.Finding, error)
}
```

Read the committed files for the doc comments on each method; they state the contract and are part of these requirements.

### Produced

```go
// internal/store
func Open(ctx context.Context, path string, logger *slog.Logger) (*DB, error)
func (db *DB) Migrate(ctx context.Context) error
func (db *DB) Close() error
func (db *DB) InTx(ctx context.Context, fn func(tx *DB) error) error
func Bind[S any](db *DB, pick func(*DB) S) func(ctx context.Context, fn func(S) error) error
var ErrSchemaTooNew = errors.New("database schema is newer than this binary")

func (db *DB) StartRun(ctx context.Context, job string, startedAt time.Time) (model.Run, error)
func (db *DB) FinishRun(ctx context.Context, run model.Run) error
func (db *DB) LatestRuns(ctx context.Context) ([]model.Run, error)
func (db *DB) UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error)
func (db *DB) ResolveFinding(ctx context.Context, fingerprint string, at time.Time) error
func (db *DB) ListOpenFindings(ctx context.Context) ([]model.Finding, error)
```

## Requirements

1. `Open`, `Migrate`, `InTx`, `Bind`, the time helpers and the method conventions follow the sqlite-store skill and its `templates/open-and-migrate.md` exactly: DSN pragmas, plain-path check, read-back of `journal_mode` and `foreign_keys`, pool size 4, `schema_migrations` with version, name, checksum and applied time, one transaction per migration file, `ErrSchemaTooNew`, checksum-changed refusal, ISO-8601 UTC second-precision timestamps, `created_ts`/`updated_ts` from the store's injected clock.
2. `Open` creates the parent directory of `path` if it is missing, with mode 0700, and creates the database file with mode 0600.
3. The `ErrSchemaTooNew` error text, as returned by `Migrate`, says the database is newer than the binary and gives both versions, for example `database schema is newer than this binary: database at version 7, binary knows 1`.
4. `migrations/0001_initial_schema.sql` creates every table in `docs/database.md` ("Tables and columns", including the accepted additions: `llm_verdicts.request_payload`, `llm_verdicts.evidence_verified`, the `digests` table with `error`, the `settings` table) and the `dep_state` view, with:
   - parent tables before children, real `REFERENCES` clauses for every `*_id` column that names another table (SQLite rejects writes to a table whose foreign key names a table that does not exist, which is why the whole schema lands in one migration; see Rulings);
   - every `UNIQUE` constraint the doc states;
   - `created_ts TEXT NOT NULL` and `updated_ts TEXT NOT NULL` on every table except `schema_migrations`, `kv`, `http_cache` and `settings` (which keep exactly the doc's columns plus `updated_ts`);
   - nullable foreign keys on `findings` (`repo_id`, `package_id`, `dep_version_id`, `advisory_id`, `issue_id`, `verdict_id`), because a `job_failure` finding has none of them;
   - `runs.status` and `findings.kind` and `finding_state.state` as `TEXT NOT NULL` with a `CHECK` listing the values of the corresponding `model` constants;
   - indexes only for the queries this work order writes (`runs(job, id)`, `findings(resolved_ts)`), plus the index SQLite needs on each foreign-key child column that a parent delete would scan. Later milestones add their own.
   `schema_migrations` itself is created by `Migrate`, not by a migration file.
5. Run methods:
   - `StartRun` inserts a row with status `model.RunRunning` and returns it with its ID.
   - `FinishRun` updates `finished_ts`, `status`, `stats` (JSON object; an empty or nil `Stats` is stored as `{}`) and `error` for `run.ID`; an unknown ID returns an error wrapping `model.ErrNotFound`.
   - `LatestRuns` returns the most recent run (highest ID) of each job, ordered by job name; nil when there are none.
6. Finding methods:
   - `UpsertFinding` keys on `f.Fingerprint`. New fingerprint: insert the finding with `first_seen_ts` and `last_changed_ts` from `f.LastChangedAt`, and a `finding_state` row with state `model.StateOpen`, both in one transaction. Existing fingerprint: update `score`, `rank_reason`, `last_changed_ts`, clear `resolved_ts`, and leave `first_seen_ts`, `reported_in_digest_ts` and the `finding_state` row untouched. Zero-valued ID fields on `f` (`RepoID` and the rest) are stored as NULL. Returns the stored finding including its `State`.
   - `ResolveFinding` sets `resolved_ts = at` and `last_changed_ts = at` on the unresolved finding with that fingerprint; no matching unresolved finding is not an error.
   - `ListOpenFindings` returns findings with `resolved_ts IS NULL` whose state is `open` or `acked`, highest `score` first, then lowest ID; each with its `State`; nil when there are none.
7. `Close` closes the pool. After `Close`, methods return an error; they do not panic.

## Tests to include

Use the store test template (`go-test` skill, `templates/store-tests.md`): real temp-file database, `newTestDB`, fixed `t0`.

- `TestOpen_Pragmas`: `journal_mode` is `wal`, `foreign_keys` is 1.
- `TestOpen_RejectsURIPath`: paths containing `?` or starting with `file:` → error.
- `TestMigrate_FreshAndIdempotent`: first `Migrate` writes one `schema_migrations` row per embedded file; a second `Migrate` (and a second `Open`+`Migrate` on the same file) changes nothing.
- `TestMigrate_SchemaTooNew`: after inserting a `schema_migrations` row with version 9999, `Migrate` returns an error for which `errors.Is(err, ErrSchemaTooNew)` and whose text contains `9999`.
- `TestMigrate_ChecksumChanged`: altering a stored checksum → `Migrate` refuses.
- `TestMigrate_FailureLeavesNothing`: a failing migration (inject through an unexported seam that takes an `fs.FS`) leaves neither its tables nor its `schema_migrations` row.
- `TestSchema_ForeignKeysEnforced`: inserting a `finding_state` row for a missing finding fails.
- `TestSchema_AllDocTables`: every table name listed in `docs/database.md` exists in `sqlite_master` (hard-code the list in the test).
- `TestDB_StartFinishRun`, `TestDB_FinishRun_NotFound`, `TestDB_LatestRuns` (three jobs, several runs each, plus empty).
- `TestDB_UpsertFinding_InsertThenUpdate` (state row created once; `first_seen_ts` kept; `resolved_ts` cleared on re-upsert), `TestDB_UpsertFinding_JobFailureHasNullRefs`, `TestDB_ResolveFinding` (resolves; second call and unknown fingerprint are no-ops), `TestDB_ListOpenFindings` (order; resolved, snoozed and dismissed excluded; write the non-open states with raw SQL in the test, since no method sets them yet).
- `TestInTx_CommitRollbackPanic`, `TestInTx_Nested`, `TestBind`.
- `TestTimestamps_RoundTrip`: a non-UTC time with nanoseconds comes back as the same instant truncated to the second, in UTC; a zero time is stored as NULL and read back zero.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/store/` | `ok` |
| `go test -race -run 'TestMigrate|TestSchema' -v ./internal/store/ \| grep -c '^--- PASS'` | at least 6 |
| `go vet ./internal/store/` with `var _ pipeline.RunStore = (*DB)(nil)` style assertions placed in an external test file (`store_test` package) for all four consumed interfaces | compiles |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- `Backup` and `VACUUM INTO` (M2, UPW-56), `Export` (M5).
- Store methods for any table other than `runs`, `findings`, `finding_state`.
- Finding state transitions, ranking, digests (M2).
- Opening the database from `cmd/upw` (WO-07).
- Editing `docs/database.md`; list needed doc changes in your report.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `sqlite-store`.
Read: `docs/database.md` (whole file); `docs/data-access.md`; `.claude/skills/sqlite-store/templates/open-and-migrate.md`; `.claude/skills/go-test/templates/store-tests.md`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: The whole documented schema goes into `0001_initial_schema.sql`. Checked with sqlite3 3.54: with `foreign_keys=1`, an insert into a table whose `REFERENCES` names a missing table fails with `no such table`, even when the child key is NULL, so `findings` cannot be created ahead of the tables it references, and constraints cannot be added later under the additive-only rule. Later milestones add columns and indexes with new migrations.
- 2026-11-19: Where `docs/database.md` leaves a column's type or nullability unstated, choose by the sqlite-store conventions (`*_ts` TEXT, flags INTEGER, JSON as TEXT) and list each choice under Decisions.
- 2026-11-19 (fix round 1): Foreign keys to `dep_versions` get explicit delete behaviour, because a full reindex of a repo deletes and rebuilds its intervals. Derived rows go with the interval: `advisory_matches.dep_version_id` and `issue_candidates.dep_version_id` are `ON DELETE CASCADE` (both are recomputed by their jobs). Operator-facing rows survive: `findings.dep_version_id` is `ON DELETE SET NULL` (the fingerprint keeps the finding's identity and the next upsert relinks it). Every other foreign key keeps the default `NO ACTION`; code that removes a repo or reindexes deletes children before parents (`dep_versions`, then `dep_events`, then `commits`). Add `TestSchema_DepVersionDeleteBehaviour`: deleting a `dep_versions` row removes its `advisory_matches` and `issue_candidates` rows, nulls `findings.dep_version_id` and keeps the finding and its `finding_state` row; deleting a `commits` row that an interval references fails.
- 2026-11-19: `applied_ts`, the exported `ErrMigrationChanged` and the `ErrSchemaTooNew` text are accepted as built.
