package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BackupTo writes a consistent, compacted copy of the database to path with
// VACUUM INTO, while other readers and writers carry on. It refuses, writing
// nothing, when path already exists. A missing parent directory is created
// with mode 0700, and the copy gets mode 0600. It must not be called on a *DB
// inside a transaction, since VACUUM cannot run in one.
func (db *DB) BackupTo(ctx context.Context, path string) error {
	if db.inTx {
		return errors.New("backup: called inside a transaction")
	}
	// SQLite reads a "file:" name as a URI, which would send the copy
	// somewhere other than the file created below; Open refuses the same.
	if path == "" || strings.ContainsRune(path, '?') || strings.HasPrefix(path, "file:") {
		return fmt.Errorf("backup: unsupported path %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("backup to %s: create directory: %w", path, err)
	}
	// Creating the file first, exclusively, refuses an existing path without a
	// check-then-write race and gives the copy its mode before any data is in
	// it. VACUUM INTO accepts an empty target file.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("backup to %s: file already exists", path)
	}
	if err != nil {
		return fmt.Errorf("backup to %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return errors.Join(fmt.Errorf("backup to %s: %w", path, err), os.Remove(path))
	}
	if _, err := db.pool.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return errors.Join(fmt.Errorf("backup to %s: %w", path, err), os.Remove(path))
	}
	// The umask can only narrow the mode given at creation; this makes the
	// result exactly 0600 whatever SQLite did with the file.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("backup to %s: set mode: %w", path, err)
	}
	return nil
}
