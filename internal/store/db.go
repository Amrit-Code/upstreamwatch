package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
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
// on, and IMMEDIATE write transactions. A missing parent directory is created
// with mode 0700 and a missing database file with mode 0600. Open does not
// migrate; call Migrate.
func Open(ctx context.Context, path string, logger *slog.Logger) (*DB, error) {
	if path == "" || strings.ContainsRune(path, '?') || strings.HasPrefix(path, "file:") {
		return nil, fmt.Errorf("open store: unsupported database path %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("open store: create directory: %w", err)
	}
	// SQLite would create the file with mode 0644; creating it first fixes the
	// mode, and SQLite gives the -wal and -shm files the database file's mode.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	switch {
	case err == nil:
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("open store: create database file: %w", err)
		}
	case !errors.Is(err, fs.ErrExist):
		return nil, fmt.Errorf("open store: create database file: %w", err)
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
		return nil, errors.Join(fmt.Errorf("open store: read journal_mode: %w", err), pool.Close())
	}
	if err := pool.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return nil, errors.Join(fmt.Errorf("open store: read foreign_keys: %w", err), pool.Close())
	}
	if journal != "wal" || fk != 1 {
		return nil, errors.Join(fmt.Errorf("open store: pragmas not applied (journal_mode=%s, foreign_keys=%d)", journal, fk), pool.Close())
	}
	return &DB{pool: pool, q: pool, now: time.Now, logger: logger}, nil
}

// Close closes the connection pool. Methods called afterwards return an error.
func (db *DB) Close() error { return db.pool.Close() }

// InTx runs fn in one write transaction (BEGIN IMMEDIATE). fn receives a *DB
// whose methods all run inside the transaction. InTx commits when fn returns
// nil and rolls back when it returns an error or panics. Called on a *DB that
// is already inside a transaction, it joins that transaction.
func (db *DB) InTx(ctx context.Context, fn func(tx *DB) error) error {
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
func Bind[S any](db *DB, pick func(*DB) S) func(ctx context.Context, fn func(S) error) error {
	return func(ctx context.Context, fn func(S) error) error {
		return db.InTx(ctx, func(tx *DB) error { return fn(pick(tx)) })
	}
}

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

// nullID stores a zero ID as NULL.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// nullText stores an empty string as NULL.
func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
