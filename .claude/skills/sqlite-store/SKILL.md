---
name: sqlite-store
description: How to work inside internal/store, the only upstreamwatch package that runs SQL. Covers opening SQLite through modernc.org/sqlite with the WAL, busy_timeout and foreign-key pragmas, single-writer connection settings, embedded forward-only migrations tracked in schema_migrations, store methods that take and return internal/model types, scan helpers, ISO-8601 UTC timestamps, the InTx transaction helper and when a write needs it, mapping sql.ErrNoRows to a model sentinel, refusing to start on a newer schema, VACUUM INTO backups, and adding a table end to end. Triggers on new table, add column, schema change, migration, schema_migrations, store method, query, InTx, transaction, "database is locked", SQLITE_BUSY, sql.ErrNoRows, pragma, backup or VACUUM INTO. Not for where store interfaces are declared or which service may use them (use upw-architecture), test helpers for store tests (go-test), or updating docs/database.md (docs-sync).
---

# Working in internal/store

`store.DB` is the database access layer. The store interfaces, the table-to-interface map and the writer rules are in `docs/data-access.md`; every table and column is in `docs/database.md`. Code scaffolds: `templates/open-and-migrate.md` (Open, Migrate, InTx, Backup) and `templates/new-table.md` (one table end to end).

Driver facts below were checked against the `modernc.org/sqlite` v1.62.0 source (the Open doc comment in `driver.go`, plus `tx.go` and `error.go`) and by running that version. Recheck them if `go.mod` moves to a different version.

## Opening the database

- **The database file is `<data_dir>/upw.sqlite`,** opened with driver name `"sqlite"` and this DSN:
  ```
  <path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate
  ```
- **The pragmas go in the DSN, not in a one-off `Exec`.** `foreign_keys` and `busy_timeout` are per-connection settings. `database/sql` opens connections whenever it likes, and the driver runs every `_pragma` on each new connection (busy_timeout first). An `Exec` after `Open` would configure only whichever connection it happened to use.
- **The path is a plain file path, not a `file:` URI.** The driver strips the query string from a plain path and applies its own `_` keys. `Open` rejects a path that contains `?` or starts with `file:`, because either would change how the DSN is parsed.
- **After opening, `Open` reads back `PRAGMA journal_mode` and `PRAGMA foreign_keys`.** If they are not `wal` and `1`, it fails. The driver does not validate `_pragma` values, so a typo would otherwise pass silently.
- **`SetMaxOpenConns(4)` and `SetMaxIdleConns(4)`.** The driver's docs say to bound the pool: each connection carries its own page cache, and the default pool is unlimited.

## Single writer: what it means for your code

The scheduler runs jobs one at a time and is the only writer to pipeline tables. The web server writes only `finding_state`, `eval_labels` and `settings`. WAL lets readers run while a write is in progress, so page loads never wait on a job. Your code keeps this working by following four rules.

- **Every read-write transaction starts with `BEGIN IMMEDIATE`** (that is what `_txlock=immediate` does). It takes the write lock at the start and waits up to busy_timeout if another writer holds it. A deferred transaction that reads first and writes later cannot wait in WAL mode: it fails with `SQLITE_BUSY` as soon as someone else has committed.
- **Keep write transactions short.** Gather everything first: network calls, git output, LLM verdicts. Then open `InTx`, write, and commit. A web write (an ack) waits behind an open job transaction, and after 5 seconds it fails with "database is locked". If that happens, the fix is a shorter transaction, never a longer timeout.
- **Inside `InTx`, use only the `tx` value you were given.** Calling the outer `db` from inside the function opens a second connection. That connection waits for your own write lock and fails after 5 seconds.
- **For several reads that must agree with each other, use one read-only transaction:** `BeginTx(ctx, &sql.TxOptions{ReadOnly: true})`. The driver starts read-only transactions with plain `BEGIN`, so they do not take the write lock.

## Migrations

- **Files are `internal/store/migrations/NNNN_short_name.sql`.** The lead assigns the filename and number in the work order: use exactly the filename the work order gives, never "highest existing plus one", so parallel builders do not collide. Numbers are four digits and never reused. Each file holds one logical change and starts with a comment saying what and why.
- **They are embedded with `//go:embed migrations/*.sql`** and applied by `Migrate` at startup, in version order. Each file and its `schema_migrations` row are committed in one transaction, so a failing migration leaves nothing behind. `cmd/upw` calls `Migrate` before any command touches data.
- **`schema_migrations` holds version, name, checksum and applied time.** `Migrate` refuses to run in two cases. First, when the database holds a version higher than any embedded file: the binary is older than the database (`ErrSchemaTooNew`; `mvp-plan/PLAN.md` section 8 depends on this for safe rollback). Second, when an applied file's checksum has changed.
- **Never edit a migration once it has been merged.** It has already run against real databases (the dev database, the VM), and an edit would never reach them. Fix a mistake with a new migration.
- **Additive only until 1.0:** `CREATE TABLE`, `CREATE INDEX`, `ALTER TABLE ... ADD COLUMN`. No dropping or renaming of tables or columns, and no type changes. If a change seems to need one, stop and report; the lead decides, because it needs a backup step before deploy.
- **`ADD COLUMN` on a table that has rows** cannot be `NOT NULL` without a constant default, cannot have a non-constant default such as `CURRENT_TIMESTAMP`, and cannot be `UNIQUE` or `PRIMARY KEY`. SQLite enforces the first two only when the table already has rows. A migration can therefore pass every test on a fresh database and fail on the VM. Test each `ALTER` against a database that has data (see `templates/new-table.md`).
- **Never put `VACUUM` or a `PRAGMA` in a migration.** `VACUUM` cannot run inside a transaction. Most pragmas either do nothing inside one or belong in the DSN.

## Store methods

- **Methods live on `*DB`.** They use plain SQL with `?` placeholders: no ORM, no query builder. Every method takes `ctx` and uses the `...Context` calls. Method names follow the lead's interface files: `Get…`, `List…`, `Upsert…`, `Save…`, `Delete…`.
- **Never build SQL from values with `fmt.Sprintf`.** The one dynamic part allowed is a generated `?,?,?` list for an `IN (...)` clause, with the values still bound. Chunk long lists at 500.
- **Store methods take and return `internal/model` types.** No `*sql.Rows`, no `sql.Null*` and no row structs leave the package. Conversions happen in the scan helper.
- **Each table has one column-list constant and one scan helper** (`repoColumns` and `scanRepo(scanner)`), shared by every query on that table. Every `SELECT` and `INSERT` names its columns; never `SELECT *`. An added column must not shift what an existing query scans.
- **`sql.ErrNoRows` becomes `model.ErrNotFound` at the store boundary,** wrapped with what was looked up: `fmt.Errorf("repo %q: %w", slug, model.ErrNotFound)`. Callers test it with `errors.Is` and never see `database/sql`.
- **Upserts use `INSERT ... ON CONFLICT(<unique key>) DO UPDATE ... RETURNING id`,** so rerunning a job never duplicates rows. Where a unique violation is an error the caller must handle, detect it with `errors.AsType[*sqlite.Error](err)` and `Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE` (from `modernc.org/sqlite/lib`; the driver turns on extended result codes). Then return a `model` sentinel.
- **Close rows with `defer rows.Close()`, and check `rows.Err()` after the loop.** List methods append to a nil slice, so "no rows" is `nil`, and tests expect `nil`.
- **JSON columns (`labels`, `features`, `raw`, `request_payload`) are TEXT holding JSON,** marshalled from and unmarshalled into model types in the store. Do not use SQLite JSON functions in queries the jobs run on every tick; the plan keeps the SQL portable.
- **Booleans are INTEGER 0/1.** A nullable flag such as `direct` (1, 0 or NULL for unknown) becomes a pointer or a three-state type in `model`, never a bare `bool`.
- **The store never encrypts or decrypts.** `settings.Service` encrypts the LLM key and the store keeps the ciphertext. The store never logs column values.

## Timestamps

- **Store times as ISO-8601 UTC text with second precision and a fixed width:** `2026-03-01T12:00:00Z`, produced by one helper: `t.UTC().Truncate(time.Second).Format(time.RFC3339)`. Fixed width means text comparison and `ORDER BY` follow time order. A variable-length fraction would sort `…05.5Z` before `…05Z`.
- **Never pass a `time.Time` to the driver.** Without `_time_format` it writes Go's `String()` form (`2026-03-01 12:00:00 +0000 UTC`). Do not write times with SQLite's `datetime('now')` either; it returns `2026-03-01 12:00:00`, with a space and no zone. Both break comparisons with our own values.
- **NULL maps to the zero `time.Time`** and back, through the scan helper.
- **When comparing an outside timestamp with a stored one** (the OSV modified time, a GitHub issue update time), truncate both to seconds first. Otherwise a record with fractional seconds looks newer on every run and is fetched again every time.
- **Timestamp columns end in `_ts` and hold ISO-8601 UTC text.** Every table has `created_ts` and `updated_ts`, except `kv`, `http_cache` and `settings` (only `updated_ts`) and `schema_migrations` (`applied_ts`).
- **`created_ts` and `updated_ts` are set by the store from its injected `now func() time.Time`,** not by SQL defaults, so tests control them.

## Transactions: InTx

`InTx(ctx, fn func(tx *DB) error) error` runs `fn` in one write transaction. The `tx` value is a copy of `*DB` whose methods all run inside that transaction, so it satisfies every store interface. `InTx` commits if `fn` returns nil and rolls back if it returns an error or panics. A call made while already inside a transaction joins the outer one. A store method that needs several statements to land together can therefore call `InTx` itself and still compose into a caller's transaction.

Services reach it without importing `store`. The service-side shape is in the lead's `interfaces.go`. Where the work order leaves it open, use this default:

- the service takes an `inTx func(ctx context.Context, fn func(Store) error) error` dependency, where `Store` is its own interface;
- `cmd/upw` builds that function with `store.Bind(db, func(d *store.DB) issues.Store { return d })`;
- a fake is `func(ctx context.Context, fn func(issues.Store) error) error { return fn(fake) }`.

**A write needs `InTx` when:**

- **Rows must land all or nothing.** An issues page and its cursor: a cursor that advances without its issues loses them forever. Events for a commit and `last_indexed_sha`. Deleting and rebuilding a repo's `dep_versions`. A verdict and its finding. A digest row and `reported_in_digest_ts`.
- **A read decides the write.** Reading a finding's state, checking the transition, writing the new state. The read has to be inside the same transaction, or another writer can change the row in between.
- **Many rows are written in one unit.** One transaction per unit (per repo, per page) is far faster than one autocommit per row. Keep it per unit, so the write lock is never held for a whole job.

A single `INSERT`, `UPDATE` or upsert statement is atomic on its own and does not need `InTx`.

## Backups

- **`Backup(ctx, path)` runs `VACUUM INTO ?` on the pool, outside any transaction.** It produces a consistent, compacted copy while readers and writers continue. Never copy the live file: in WAL mode, recent commits live in `upw.sqlite-wal`.
- **The target must not exist.** SQLite refuses with "output file already exists". Write to `<name>.tmp`, remove any stale `.tmp` first, and rename into place when the copy finishes.
- **Naming and retention belong to the `backup` job (`ops.Backup`),** following `mvp-plan/PLAN.md` section 3: `data/backups/upw-YYYYMMDD.sqlite`, keep 7.
- **To restore:** stop `upw`, replace `upw.sqlite` with the backup, delete `upw.sqlite-wal` and `upw.sqlite-shm`, then start. `Migrate` brings an older backup forward.

## Adding a table end to end

1. **Get the columns from the work order.** It should match, or be about to change, `docs/database.md`. If neither gives the columns, stop and ask.
2. **Write a migration** with `CREATE TABLE` and these parts:
   - `id INTEGER PRIMARY KEY`;
   - foreign keys with `REFERENCES`;
   - the `UNIQUE` constraint your upsert's `ON CONFLICT` targets;
   - `created_ts TEXT NOT NULL` and `updated_ts TEXT NOT NULL`;
   - an index for each `WHERE` and `ORDER BY` your queries use.
3. **Add the model type** in `internal/model`, if your work order lists it: plain Go types, no `db` tags.
4. **Write the store code:** the column constant, the scan helper, then the methods.
5. **Check the consumer's `interfaces.go` lists the methods.** That file is the lead's. If a method is missing, report it rather than editing the file.
6. **Write the tests,** using `go-test`'s store template: a round trip, not found, idempotent upsert, constraint failure, and the migration applied to a populated database.
7. **Name the doc updates in your report:** the table and columns in `docs/database.md`, and its row in the table-to-interface map in `docs/data-access.md`. A schema change is not finished until `docs-sync` has applied them.

## Not covered here

- Which service may use which store interface, and where interfaces are declared: `upw-architecture`
- The store test helper and the per-method test checklist: `go-test`
- Go style, errors and logging: `go-code`
- Bringing `docs/database.md` and `docs/data-access.md` in line: `docs-sync`
- Backup schedule and deployment volumes: `upw-deploy`
