# Scaffold: store.DB core (Open, Migrate, InTx, Bind, Backup)

Use this when building or changing the core of `internal/store`, which is M0 work. Method sets for the individual tables come from the lead's interface files and `templates/new-table.md`.

## Contents

- The DB type and Open
- Migrate
- InTx and Bind
- Backup
- Time helpers

## The DB type and Open

```go
// Package store is the database access layer: the only package that runs SQL.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// querier is satisfied by both *sql.DB and *sql.Tx, so every method runs the
// same way inside or outside a transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// DB is the database access layer. A *DB returned by InTx runs every method
// inside that transaction.
type DB struct {
	pool   *sql.DB
	q      querier
	inTx   bool
	now    func() time.Time
	logger *slog.Logger
}

const dsnParams = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"

// Open opens the SQLite file at path with WAL, a 5 s busy timeout, foreign keys
// on, and IMMEDIATE write transactions. It does not migrate; call Migrate.
func Open(ctx context.Context, path string, logger *slog.Logger) (*DB, error) {
	if path == "" || strings.ContainsRune(path, '?') || strings.HasPrefix(path, "file:") {
		return nil, fmt.Errorf("open store: unsupported database path %q", path)
	}
	pool, err := sql.Open("sqlite", path+dsnParams)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	pool.SetMaxOpenConns(4)
	pool.SetMaxIdleConns(4)

	var journal string
	var fk int
	if err := pool.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		pool.Close()
		return nil, fmt.Errorf("open store: read journal_mode: %w", err)
	}
	if err := pool.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		pool.Close()
		return nil, fmt.Errorf("open store: read foreign_keys: %w", err)
	}
	if journal != "wal" || fk != 1 {
		pool.Close()
		return nil, fmt.Errorf("open store: pragmas not applied (journal_mode=%s, foreign_keys=%d)", journal, fk)
	}
	return &DB{pool: pool, q: pool, now: time.Now, logger: logger}, nil
}

// Close closes the connection pool.
func (db *DB) Close() error { return db.pool.Close() }
```

## Migrate

```go
//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrSchemaTooNew means the database was migrated by a newer upw than this one.
var ErrSchemaTooNew = errors.New("database schema is newer than this binary")

// ErrMigrationChanged means an applied migration file was edited after it ran.
var ErrMigrationChanged = errors.New("applied migration has changed")

type migration struct {
	version  int
	name     string // file name, e.g. 0003_add_digests.sql
	sql      string
	checksum string // hex sha256 of the file
}

// Migrate applies every embedded migration not yet recorded in
// schema_migrations, each in its own transaction with its record.
func (db *DB) Migrate(ctx context.Context) error {
	const ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		checksum   TEXT NOT NULL,
		applied_ts TEXT NOT NULL
	)`
	if _, err := db.pool.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	files, err := loadMigrations(migrationFiles) // sorted by version; errors on a bad name or duplicate version
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	applied, err := db.appliedMigrations(ctx) // version -> checksum
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	latest := 0
	if len(files) > 0 {
		latest = files[len(files)-1].version
	}
	for v := range applied {
		if v > latest {
			return fmt.Errorf("migrate: %w: database has version %d, binary knows up to %d", ErrSchemaTooNew, v, latest)
		}
	}

	for _, m := range files {
		if sum, ok := applied[m.version]; ok {
			if sum != m.checksum {
				return fmt.Errorf("migrate: %w: %s", ErrMigrationChanged, m.name)
			}
			continue
		}
		err := db.InTx(ctx, func(tx *DB) error {
			if _, err := tx.q.ExecContext(ctx, m.sql); err != nil { // the driver runs every statement in the file
				return fmt.Errorf("apply %s: %w", m.name, err)
			}
			_, err := tx.q.ExecContext(ctx,
				`INSERT INTO schema_migrations (version, name, checksum, applied_ts) VALUES (?, ?, ?, ?)`,
				m.version, m.name, m.checksum, formatTime(db.now()))
			return err
		})
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		db.logger.Info("migration applied", "version", m.version, "name", m.name)
	}
	return nil
}
```

`loadMigrations` reads `migrations/*.sql`, parses the four-digit prefix, computes the checksum, sorts by version, and fails on a duplicate version or a name that does not match `^\d{4}_[a-z0-9_]+\.sql$`.

## InTx and Bind

```go
// InTx runs fn in one write transaction (BEGIN IMMEDIATE). fn receives a *DB
// whose methods all run inside the transaction. InTx commits when fn returns
// nil and rolls back when it returns an error or panics. Called on a *DB that
// is already inside a transaction, it joins that transaction.
func (db *DB) InTx(ctx context.Context, fn func(tx *DB) error) (err error) {
	if db.inTx {
		return fn(db)
	}
	sqlTx, err := db.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = sqlTx.Rollback() // the panic is the error that matters
			panic(p)
		}
	}()

	tx := *db
	tx.q, tx.inTx = sqlTx, true
	if err := fn(&tx); err != nil {
		if rbErr := sqlTx.Rollback(); rbErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Bind adapts InTx to a consumer-declared store interface S, so a service can
// run its own interface inside a transaction without importing this package.
// cmd/upw calls it while wiring:
//
//	store.Bind(db, func(d *store.DB) issues.Store { return d })
func Bind[S any](db *DB, as func(*DB) S) func(context.Context, func(S) error) error {
	return func(ctx context.Context, fn func(S) error) error {
		return db.InTx(ctx, func(tx *DB) error { return fn(as(tx)) })
	}
}
```

## Backup

```go
// Backup writes a consistent, compacted copy of the database to path using
// VACUUM INTO. path must not exist; Backup writes path+".tmp" and renames it.
func (db *DB) Backup(ctx context.Context, path string) error {
	if db.inTx {
		return errors.New("backup: cannot run inside a transaction")
	}
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("backup: remove stale %s: %w", tmp, err)
	}
	if _, err := db.pool.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return fmt.Errorf("backup: vacuum into %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("backup: rename to %s: %w", path, err)
	}
	return nil
}
```

## Time helpers

```go
const timeLayout = time.RFC3339 // second precision, "Z" suffix for UTC: fixed width

// formatTime renders t for storage; the zero time is stored as NULL.
func formatTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Truncate(time.Second).Format(timeLayout)
}

// parseTime reads a stored time; NULL becomes the zero time.
func parseTime(s sql.NullString) (time.Time, error) {
	if !s.Valid || s.String == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(timeLayout, s.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time %q: %w", s.String, err)
	}
	return t.UTC(), nil
}
```

For a `NOT NULL` time column such as `created_ts`, pass `formatTime(db.now())`. It is never zero.
