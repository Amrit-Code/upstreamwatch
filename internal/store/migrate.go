package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrSchemaTooNew means the database was migrated by a newer upw than this one.
var ErrSchemaTooNew = errors.New("database schema is newer than this binary")

// ErrMigrationChanged means an applied migration file was edited after it ran.
var ErrMigrationChanged = errors.New("applied migration has changed")

type migration struct {
	version  int
	name     string // file name, e.g. 0001_initial_schema.sql
	sql      string
	checksum string // hex sha256 of the file
}

var migrationName = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)

// Migrate applies every embedded migration not yet recorded in
// schema_migrations, each in its own transaction with its record. It refuses a
// database migrated by a newer binary (ErrSchemaTooNew) and an applied
// migration whose file has changed (ErrMigrationChanged).
func (db *DB) Migrate(ctx context.Context) error {
	return db.migrate(ctx, migrationFiles)
}

// migrate is Migrate reading the migrations from fsys, so tests can supply
// their own files.
func (db *DB) migrate(ctx context.Context, fsys fs.FS) error {
	const ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		checksum   TEXT NOT NULL,
		applied_ts TEXT NOT NULL
	)`
	if _, err := db.pool.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	files, err := loadMigrations(fsys)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	applied, err := db.appliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	latest := 0
	if len(files) > 0 {
		latest = files[len(files)-1].version
	}
	newest := 0
	for v := range applied {
		newest = max(newest, v)
	}
	if newest > latest {
		return fmt.Errorf("%w: database at version %d, binary knows %d", ErrSchemaTooNew, newest, latest)
	}

	for _, m := range files {
		if sum, ok := applied[m.version]; ok {
			if sum != m.checksum {
				return fmt.Errorf("migrate: %w: %s", ErrMigrationChanged, m.name)
			}
			continue
		}
		err := db.InTx(ctx, func(tx *DB) error {
			// The driver runs every statement in the file.
			if _, err := tx.q.ExecContext(ctx, m.sql); err != nil {
				return fmt.Errorf("apply %s: %w", m.name, err)
			}
			if _, err := tx.q.ExecContext(ctx,
				`INSERT INTO schema_migrations (version, name, checksum, applied_ts) VALUES (?, ?, ?, ?)`,
				m.version, m.name, m.checksum, formatTime(db.now())); err != nil {
				return fmt.Errorf("record %s: %w", m.name, err)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		db.logger.Info("migration applied", "version", m.version, "name", m.name)
	}
	return nil
}

// loadMigrations reads migrations/*.sql from fsys, sorted by version. It fails
// on a file name that does not match NNNN_name.sql and on a duplicate version.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	names, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	files := make([]migration, 0, len(names))
	seen := make(map[int]string, len(names))
	for _, p := range names {
		name := path.Base(p)
		if !migrationName.MatchString(name) {
			return nil, fmt.Errorf("migration file name %q does not match NNNN_name.sql", name)
		}
		version, err := strconv.Atoi(name[:4])
		if err != nil {
			return nil, fmt.Errorf("migration %s: parse version: %w", name, err)
		}
		if other, ok := seen[version]; ok {
			return nil, fmt.Errorf("migrations %s and %s share version %d", other, name, version)
		}
		seen[version] = name
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		sum := sha256.Sum256(body)
		files = append(files, migration{
			version:  version,
			name:     name,
			sql:      string(body),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	slices.SortFunc(files, func(a, b migration) int { return a.version - b.version })
	return files, nil
}

// appliedMigrations returns the checksum of each applied migration by version.
func (db *DB) appliedMigrations(ctx context.Context) (map[int]string, error) {
	rows, err := db.q.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[int]string)
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[v] = sum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	return applied, nil
}
