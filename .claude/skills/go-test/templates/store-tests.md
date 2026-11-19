# Template: store tests on a real SQLite file

Use this when testing `internal/store`. Store tests run real SQL against a real database file, migrated exactly as at startup. Services never use this; they get fakes. Store conventions (column lists, scan helpers, time format, sentinels, `InTx`) are in `sqlite-store`.

## Helper

Put it in `internal/store/helpers_test.go`.

```go
// newTestDB opens and migrates a fresh database file for one test.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.sqlite")
	db, err := Open(t.Context(), path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open(%s) error = %v", path, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return db
}

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) // whole seconds: the store keeps second precision
```

One database per test means tests can run with `t.Parallel()`. A test that needs a parent row (a repo before its commits) creates it through the store's own methods, not raw SQL. That way the test also exercises those methods.

## What every store method gets

```go
func TestDB_UpsertRepo(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		want := model.TargetRepo{Slug: "server", URL: "https://github.com/example-org/example-server", DefaultBranch: "main", LastFetchAt: t0}
		id, err := db.UpsertRepo(ctx, want)
		if err != nil {
			t.Fatalf("UpsertRepo() error = %v", err)
		}
		got, err := db.RepoBySlug(ctx, "server")
		if err != nil {
			t.Fatalf("RepoBySlug() error = %v", err)
		}
		want.ID = id
		if !reflect.DeepEqual(got, want) {
			t.Errorf("RepoBySlug() = %+v, want %+v", got, want)
		}
	})

	t.Run("missing row maps to model.ErrNotFound", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		_, err := db.RepoBySlug(ctx, "nope")
		if !errors.Is(err, model.ErrNotFound) {
			t.Errorf("RepoBySlug() error = %v, want model.ErrNotFound", err)
		}
	})

	t.Run("upsert twice keeps one row", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		r := model.TargetRepo{Slug: "server", URL: "https://github.com/example-org/example-server"}
		first, _ := db.UpsertRepo(ctx, r)
		r.DefaultBranch = "trunk"
		second, err := db.UpsertRepo(ctx, r)
		if err != nil || second != first {
			t.Errorf("second UpsertRepo() = %d, %v; want %d, nil", second, err, first)
		}
	})
}
```

The checklist for each method:

- **Round trip:** saved values come back equal, including times (UTC, whole seconds) and nil versus empty for optional fields.
- **Not found** returns the `model` sentinel, never `sql.ErrNoRows`.
- **Idempotency:** running an upsert twice leaves one row. Rerunning a job must not duplicate data.
- **Constraints:** a foreign key to a missing parent fails, which proves `foreign_keys` is on for every connection, not just the first.
- **Ordering:** list methods return rows in the documented order. Insert out of order to prove it.

## Transactions and migrations

```go
func TestDB_InTx_RollsBackOnError(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	boom := errors.New("boom")
	err := db.InTx(t.Context(), func(tx *DB) error {
		if _, err := tx.UpsertRepo(t.Context(), model.TargetRepo{Slug: "server", URL: "https://github.com/example-org/example-server"}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx() error = %v, want boom", err)
	}
	if _, err := db.RepoBySlug(t.Context(), "server"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("row survived rollback: err = %v", err)
	}
}
```

Also in the store suite:

- `Migrate` on a fresh file applies every migration. Running it again is a no-op.
- Opening a database whose `schema_migrations` lists a migration this binary does not have fails with the schema-too-new error.
- A reader can query while a write transaction is open (WAL). A second writer waits instead of failing: run two `InTx` calls from two goroutines and expect both to succeed.
- `Backup` writes a file that opens, migrates as a no-op, and holds the same rows.
