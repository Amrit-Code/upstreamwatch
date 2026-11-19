package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// backupJobName is the job's name in the config schedules, in "upw run" and in
// the runs table.
const backupJobName = "backup"

// defaultBackupKeep is the number of backups kept when BackupOptions.Keep is
// not set.
const defaultBackupKeep = 7

// The keys of the stats a backup run reports.
const (
	statExists  = "exists"
	statWritten = "written"
	statDeleted = "deleted"
)

// backupName matches the files the backup job writes, and so the only files
// retention may delete.
var backupName = regexp.MustCompile(`^upw-\d{8}\.sqlite$`)

// BackupOptions configures a Backup.
type BackupOptions struct {
	// Dir is the directory the backups are written to, <data_dir>/backups.
	Dir string
	// Keep is the number of backups kept, retention.backups_keep; zero or
	// less means 7.
	Keep int
	// Now returns the current time; nil means time.Now. The file name uses
	// the local date of the time it returns.
	Now func() time.Time
}

// Backup is the nightly backup job: it writes Dir/upw-YYYYMMDD.sqlite once a
// day and deletes the oldest backups beyond Keep.
type Backup struct {
	db     Backuper
	dir    string
	keep   int
	now    func() time.Time
	logger *slog.Logger
}

// NewBackup returns a Backup that copies db into opts.Dir. It does no I/O.
func NewBackup(db Backuper, opts BackupOptions, logger *slog.Logger) *Backup {
	keep := opts.Keep
	if keep <= 0 {
		keep = defaultBackupKeep
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Backup{
		db:     db,
		dir:    opts.Dir,
		keep:   keep,
		now:    now,
		logger: logger.With("job", backupJobName),
	}
}

// Name returns "backup".
func (b *Backup) Name() string { return backupJobName }

// Run writes today's backup, Dir/upw-YYYYMMDD.sqlite, unless that file already
// exists, then deletes the oldest backup files until at most Keep remain.
// Dir is created with mode 0700 if missing. The stats always hold exists,
// written and deleted. Only regular files named like a backup are deleted;
// when a deletion fails the others are still tried and the failures are
// returned together. scope is ignored.
func (b *Backup) Run(ctx context.Context, _ model.Scope) (model.Stats, error) {
	stats := model.Stats{statExists: 0, statWritten: 0, statDeleted: 0}
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return stats, fmt.Errorf("backup: create directory %s: %w", b.dir, err)
	}

	path := filepath.Join(b.dir, "upw-"+b.now().Local().Format("20060102")+".sqlite")
	_, err := os.Stat(path)
	switch {
	case err == nil:
		stats[statExists] = 1
	case errors.Is(err, fs.ErrNotExist):
		if err := b.db.BackupTo(ctx, path); err != nil {
			return stats, fmt.Errorf("backup: %w", err)
		}
		stats[statWritten] = 1
	default:
		return stats, fmt.Errorf("backup: check %s: %w", path, err)
	}

	deleted, err := b.prune()
	stats[statDeleted] = int64(deleted)
	b.logger.Info("backup finished", "path", path,
		"exists", stats[statExists], "written", stats[statWritten], "deleted", deleted)
	if err != nil {
		return stats, fmt.Errorf("backup: retention: %w", err)
	}
	return stats, nil
}

// prune deletes the oldest backup files in the directory until at most keep
// remain, and returns how many it deleted. A failed deletion does not stop the
// others; every failure is in the returned error.
func (b *Backup) prune() (int, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return 0, fmt.Errorf("list %s: %w", b.dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && backupName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	if len(names) <= b.keep {
		return 0, nil
	}
	// The date in the name sorts the same as the name, oldest first.
	slices.Sort(names)
	deleted := 0
	var errs []error
	for _, name := range names[:len(names)-b.keep] {
		if err := os.Remove(filepath.Join(b.dir, name)); err != nil {
			errs = append(errs, err)
			continue
		}
		deleted++
	}
	return deleted, errors.Join(errs...)
}
