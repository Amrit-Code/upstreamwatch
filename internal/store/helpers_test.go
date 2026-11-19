package store

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) // whole seconds: the store keeps second precision

// newTestDB opens and migrates a fresh database file for one test. Its clock
// is fixed at t0.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.sqlite")
	db := openTestDB(t, path)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return db
}

// openTestDB opens path without migrating and closes it at cleanup.
func openTestDB(t *testing.T, path string) *DB {
	t.Helper()
	db, err := Open(t.Context(), path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open(%s) error = %v", path, err)
	}
	db.now = func() time.Time { return t0 }
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return db
}

// exec runs a raw statement for test setup and fails the test on error.
func exec(t *testing.T, db *DB, query string, args ...any) {
	t.Helper()
	if _, err := db.pool.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// count returns the result of a SELECT COUNT(*) query.
func count(t *testing.T, db *DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.pool.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}
