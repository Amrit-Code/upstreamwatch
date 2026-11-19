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
	pool *sql.DB
	q    querier
	inTx bool
	// writeQ is the write queue shared by every *DB derived from one Open: a
	// channel with a buffer of one, held (one value in the buffer) for the life
	// of an outermost InTx or of one single-statement write. A goroutine that
	// cannot send parks on the channel's send queue, which the runtime serves
	// first in, first out, and a receive
	// hands the slot straight to the head of that queue, so a later arrival
	// cannot overtake a waiter. Unlike sync.Mutex it can also be abandoned in a
	// select when the caller's context ends.
	writeQ chan struct{}
	now    func() time.Time
	logger *slog.Logger
}

// busyTimeout is how long SQLite waits for a write lock held by another
// process before failing with SQLITE_BUSY.
const busyTimeout = 5 * time.Second

// dsnParams returns the DSN query string: WAL, foreign keys on, IMMEDIATE
// write transactions, and the given busy timeout.
func dsnParams(busy time.Duration) string {
	return fmt.Sprintf("?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate",
		busy.Milliseconds())
}

// Open opens the SQLite file at path with WAL, a 5 s busy timeout, foreign keys
// on, and IMMEDIATE write transactions. A missing parent directory is created
// with mode 0700, an existing one that group or others can access is set to
// 0700, and a missing database file is created with mode 0600. Open does not
// migrate; call Migrate.
func Open(ctx context.Context, path string, logger *slog.Logger) (*DB, error) {
	return open(ctx, path, busyTimeout, logger)
}

// open is Open with the busy timeout as a parameter, so tests can shorten it.
func open(ctx context.Context, path string, busy time.Duration, logger *slog.Logger) (*DB, error) {
	if path == "" || strings.ContainsRune(path, '?') || strings.HasPrefix(path, "file:") {
		return nil, fmt.Errorf("open store: unsupported database path %q", path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("open store: create directory: %w", err)
	}
	if err := tightenDir(dir); err != nil {
		return nil, fmt.Errorf("open store: %w", err)
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

	pool, err := sql.Open("sqlite", path+dsnParams(busy))
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
	return &DB{pool: pool, q: pool, writeQ: make(chan struct{}, 1), now: time.Now, logger: logger}, nil
}

// tightenDir sets dir to mode 0700 when it grants any permission to group or
// others, and leaves a 0700 or stricter directory alone.
func tightenDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat data directory %s: %w", dir, err)
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("data directory %s must not be accessible to group or others: %w", dir, err)
	}
	return nil
}

// Close closes the connection pool. Methods called afterwards return an error.
func (db *DB) Close() error { return db.pool.Close() }

// InTx runs fn in one write transaction (BEGIN IMMEDIATE). fn receives a *DB
// whose methods all run inside the transaction. InTx commits when fn returns
// nil and rolls back when it returns an error or panics. Called on a *DB that
// is already inside a transaction, it joins that transaction.
//
// Writes from one process take turns in arrival order: InTx waits for the
// write queue before BEGIN and leaves it after commit or rollback, and every
// single-statement write method outside a transaction waits for the same
// queue, so SQLite's busy timeout only covers writers in other processes. If
// ctx ends while InTx waits, it returns an error wrapping ctx.Err() without
// opening a transaction. fn must use only the tx it is given: an InTx on the outer *DB
// from inside fn waits for its own transaction until ctx ends.
func (db *DB) InTx(ctx context.Context, fn func(tx *DB) error) error {
	if db.inTx {
		return fn(db)
	}
	if err := db.enterWriteQueue(ctx); err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Registered before the rollback defer below, so it runs after it, also
	// when fn panics.
	defer db.leaveWriteQueue()

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

// write runs fn, a single autocommit write statement, holding the write queue,
// so it takes its turn behind any InTx of this process instead of meeting an
// open transaction in SQLite's busy handler. It waits as InTx does: if ctx
// ends first it returns an error wrapping ctx.Err() and fn does not run.
// Called on a *DB inside a transaction it runs fn directly, since that
// transaction already holds the queue.
func (db *DB) write(ctx context.Context, fn func() error) error {
	if db.inTx {
		return fn()
	}
	if err := db.enterWriteQueue(ctx); err != nil {
		return err
	}
	defer db.leaveWriteQueue()
	return fn()
}

// enterWriteQueue waits for the write queue or for ctx to end.
func (db *DB) enterWriteQueue(ctx context.Context) error {
	select {
	case db.writeQ <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for write queue: %w", ctx.Err())
	}
}

// leaveWriteQueue hands the write queue to the next waiter.
func (db *DB) leaveWriteQueue() { <-db.writeQ }

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
