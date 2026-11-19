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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestOpen_Pragmas(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	// Check every pooled connection, not only the first one opened.
	conns := make([]*sql.Conn, 0, 4)
	for range 4 {
		c, err := db.pool.Conn(t.Context())
		if err != nil {
			t.Fatalf("Conn() error = %v", err)
		}
		conns = append(conns, c)
	}
	for i, c := range conns {
		var journal string
		var fk int
		if err := c.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&journal); err != nil {
			t.Fatalf("conn %d: PRAGMA journal_mode error = %v", i, err)
		}
		if err := c.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("conn %d: PRAGMA foreign_keys error = %v", i, err)
		}
		if journal != "wal" {
			t.Errorf("conn %d: journal_mode = %q, want wal", i, journal)
		}
		if fk != 1 {
			t.Errorf("conn %d: foreign_keys = %d, want 1", i, fk)
		}
	}
	for _, c := range conns {
		if err := c.Close(); err != nil {
			t.Errorf("conn Close() error = %v", err)
		}
	}
}

func TestOpen_CreatesDirAndFileWithModes(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "data", "nested")
	path := filepath.Join(dir, "upw.sqlite")
	db := openTestDB(t, path)
	if err := db.Migrate(t.Context()); err != nil { // a write, so the -wal and -shm files exist
		t.Fatalf("Migrate() error = %v", err)
	}

	for _, tc := range []struct {
		path string
		want fs.FileMode
	}{
		{filepath.Dir(dir), 0o700},
		{dir, 0o700},
		{path, 0o600},
		{path + "-wal", 0o600},
		{path + "-shm", 0o600},
	} {
		info, err := os.Stat(tc.path)
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", tc.path, err)
		}
		if got := info.Mode().Perm(); got != tc.want {
			t.Errorf("mode of %s = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestOpen_TightensExistingDataDir(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on windows")
	}
	tests := []struct {
		name string
		mode fs.FileMode
		want fs.FileMode
	}{
		{name: "world readable", mode: 0o755, want: 0o700},
		{name: "group readable", mode: 0o750, want: 0o700},
		{name: "others writable only", mode: 0o702, want: 0o700},
		{name: "already private", mode: 0o700, want: 0o700},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "data")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
			// Chmod is not subject to the umask, so the mode is exact.
			if err := os.Chmod(dir, tt.mode); err != nil {
				t.Fatalf("Chmod(%v) error = %v", tt.mode, err)
			}
			openTestDB(t, filepath.Join(dir, "upw.sqlite"))
			info, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("Stat(%s) error = %v", dir, err)
			}
			if got := info.Mode().Perm(); got != tt.want {
				t.Errorf("Open() with data dir mode %v left mode %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestOpen_ExistingFileKeepsData(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "upw.sqlite")
	first, err := Open(t.Context(), path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := first.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if _, err := first.StartRun(t.Context(), "ingest", t0); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second := openTestDB(t, path)
	if got := count(t, second, "SELECT COUNT(*) FROM runs"); got != 1 {
		t.Errorf("runs after reopen = %d, want 1", got)
	}
}

func TestOpen_RejectsURIPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, path := range []string{
		"",
		filepath.Join(dir, "upw.sqlite") + "?mode=ro",
		"file:" + filepath.Join(dir, "upw.sqlite"),
		"file::memory:",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			db, err := Open(t.Context(), path, slog.New(slog.DiscardHandler))
			if err == nil {
				_ = db.Close() // the open was wrong; the close result does not matter
				t.Fatalf("Open(%q) error = nil, want an error", path)
			}
		})
	}
}

func TestMigrate_FreshAndIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "upw.sqlite")
	db := openTestDB(t, path)
	ctx := t.Context()

	files, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no embedded migrations")
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM schema_migrations"); got != len(files) {
		t.Errorf("schema_migrations rows = %d, want %d", got, len(files))
	}
	var name, sum, applied string
	if err := db.pool.QueryRowContext(ctx,
		"SELECT name, checksum, applied_ts FROM schema_migrations WHERE version = 1").Scan(&name, &sum, &applied); err != nil {
		t.Fatalf("read version 1: %v", err)
	}
	if name != "0001_initial_schema.sql" || sum != files[0].checksum || applied != "2026-03-01T12:00:00Z" {
		t.Errorf("version 1 row = (%q, %q, %q), want (0001_initial_schema.sql, %q, 2026-03-01T12:00:00Z)",
			name, sum, applied, files[0].checksum)
	}

	snapshot := func(d *DB) string {
		rows, err := d.pool.QueryContext(ctx,
			"SELECT version || ' ' || name || ' ' || checksum || ' ' || applied_ts FROM schema_migrations ORDER BY version")
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		defer rows.Close()
		var lines []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("snapshot scan: %v", err)
			}
			lines = append(lines, s)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("snapshot rows: %v", err)
		}
		var objects int
		if err := d.pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master").Scan(&objects); err != nil {
			t.Fatalf("snapshot objects: %v", err)
		}
		return strings.Join(lines, "\n") + "\nobjects=" + strconv.Itoa(objects)
	}
	before := snapshot(db)

	db.now = func() time.Time { return t0.Add(time.Hour) }
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	if got := snapshot(db); got != before {
		t.Errorf("second Migrate() changed schema_migrations:\n%s\nwant\n%s", got, before)
	}

	again := openTestDB(t, path)
	if err := again.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() after reopen error = %v", err)
	}
	if got := snapshot(again); got != before {
		t.Errorf("Migrate() after reopen changed schema_migrations:\n%s\nwant\n%s", got, before)
	}
}

func TestMigrate_SchemaTooNew(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	exec(t, db, `INSERT INTO schema_migrations (version, name, checksum, applied_ts)
		VALUES (9999, '9999_future.sql', 'x', '2026-03-01T12:00:00Z')`)

	err := db.Migrate(t.Context())
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Migrate() error = %v, want ErrSchemaTooNew", err)
	}
	files, err2 := loadMigrations(migrationFiles)
	if err2 != nil {
		t.Fatalf("loadMigrations() error = %v", err2)
	}
	want := fmt.Sprintf("database schema is newer than this binary: database at version 9999, binary knows %d", files[len(files)-1].version)
	if err.Error() != want {
		t.Errorf("Migrate() error = %q, want %q", err.Error(), want)
	}
}

func TestMigrate_ChecksumChanged(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	exec(t, db, "UPDATE schema_migrations SET checksum = 'edited' WHERE version = 1")

	err := db.Migrate(t.Context())
	if !errors.Is(err, ErrMigrationChanged) {
		t.Fatalf("Migrate() error = %v, want ErrMigrationChanged", err)
	}
	if !strings.Contains(err.Error(), "0001_initial_schema.sql") {
		t.Errorf("Migrate() error = %q, want it to name the file", err)
	}
}

func TestMigrate_FailureLeavesNothing(t *testing.T) {
	t.Parallel()
	initial, err := fs.ReadFile(migrationFiles, "migrations/0001_initial_schema.sql")
	if err != nil {
		t.Fatalf("read embedded migration: %v", err)
	}
	fsys := fstest.MapFS{
		"migrations/0001_initial_schema.sql": {Data: initial},
		"migrations/0002_broken.sql": {Data: []byte(
			"-- 0002_broken: a table, then a statement that fails.\n" +
				"CREATE TABLE broken_first (id INTEGER PRIMARY KEY);\n" +
				"INSERT INTO no_such_table (id) VALUES (1);\n")},
	}
	db := openTestDB(t, filepath.Join(t.TempDir(), "upw.sqlite"))

	err = db.migrate(t.Context(), fsys)
	if err == nil {
		t.Fatal("migrate() error = nil, want the failing migration's error")
	}
	if !strings.Contains(err.Error(), "0002_broken.sql") {
		t.Errorf("migrate() error = %q, want it to name 0002_broken.sql", err)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name = 'broken_first'"); got != 0 {
		t.Errorf("broken_first exists after failed migration")
	}
	if got := count(t, db, "SELECT COUNT(*) FROM schema_migrations WHERE version = 2"); got != 0 {
		t.Errorf("schema_migrations has a row for the failed migration")
	}
	if got := count(t, db, "SELECT COUNT(*) FROM schema_migrations WHERE version = 1"); got != 1 {
		t.Errorf("schema_migrations version 1 rows = %d, want 1 (applied before the failure)", got)
	}
}

func TestLoadMigrations_RejectsBadNames(t *testing.T) {
	t.Parallel()
	for name, fsys := range map[string]fstest.MapFS{
		"bad name": {"migrations/1_init.sql": {Data: []byte("SELECT 1;")}},
		"duplicate version": {
			"migrations/0001_a.sql": {Data: []byte("SELECT 1;")},
			"migrations/0001_b.sql": {Data: []byte("SELECT 1;")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := loadMigrations(fsys); err == nil {
				t.Errorf("loadMigrations() error = nil, want an error")
			}
		})
	}
}

func TestSchema_ForeignKeysEnforced(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	// Try on every pooled connection, so a connection opened without the
	// pragma would be caught.
	conns := make([]*sql.Conn, 0, 4)
	for range 4 {
		c, err := db.pool.Conn(t.Context())
		if err != nil {
			t.Fatalf("Conn() error = %v", err)
		}
		conns = append(conns, c)
	}
	for i, c := range conns {
		_, err := c.ExecContext(t.Context(),
			`INSERT INTO finding_state (finding_id, state, created_ts, updated_ts)
			 VALUES (424242, 'open', '2026-03-01T12:00:00Z', '2026-03-01T12:00:00Z')`)
		if err == nil {
			t.Errorf("conn %d: insert of finding_state for a missing finding succeeded, want a foreign-key error", i)
		}
	}
	for _, c := range conns {
		if err := c.Close(); err != nil {
			t.Errorf("conn Close() error = %v", err)
		}
	}
}

func TestSchema_AllDocTables(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	// Every table and view in docs/database.md, "Tables and columns".
	for _, tc := range []struct{ name, typ string }{
		{"repos", "table"},
		{"commits", "table"},
		{"manifests", "table"},
		{"packages", "table"},
		{"dep_events", "table"},
		{"dep_versions", "table"},
		{"dep_state", "view"},
		{"upstream_repos", "table"},
		{"package_upstream", "table"},
		{"advisories", "table"},
		{"advisory_affected", "table"},
		{"advisory_matches", "table"},
		{"upstream_issues", "table"},
		{"upstream_issue_comments", "table"},
		{"symbol_usage", "table"},
		{"issue_candidates", "table"},
		{"llm_verdicts", "table"},
		{"findings", "table"},
		{"finding_state", "table"},
		{"digests", "table"},
		{"eval_labels", "table"},
		{"settings", "table"},
		{"runs", "table"},
		{"http_cache", "table"},
		{"kv", "table"},
		{"schema_migrations", "table"},
	} {
		if got := count(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name = ? AND type = ?", tc.name, tc.typ); got != 1 {
			t.Errorf("%s %s missing from sqlite_master", tc.typ, tc.name)
		}
	}
}

func TestSchema_AcceptedAdditionsAndChecks(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	for _, tc := range []struct{ table, column string }{
		{"llm_verdicts", "request_payload"},
		{"llm_verdicts", "evidence_verified"},
		{"digests", "error"},
		{"settings", "updated_ts"},
		{"findings", "created_ts"},
		{"finding_state", "updated_ts"},
	} {
		if got := count(t, db, "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", tc.table, tc.column); got != 1 {
			t.Errorf("column %s.%s missing", tc.table, tc.column)
		}
	}

	ts := "2026-03-01T12:00:00Z"
	for name, query := range map[string]string{
		"runs.status": `INSERT INTO runs (job, started_ts, status, stats, error, created_ts, updated_ts)
			VALUES ('ingest', '` + ts + `', 'paused', '{}', '', '` + ts + `', '` + ts + `')`,
		"findings.kind": `INSERT INTO findings (kind, fingerprint, score, rank_reason, first_seen_ts, last_changed_ts, created_ts, updated_ts)
			VALUES ('rumour', 'fp', 0, '', '` + ts + `', '` + ts + `', '` + ts + `', '` + ts + `')`,
	} {
		if _, err := db.pool.ExecContext(t.Context(), query); err == nil {
			t.Errorf("%s: insert of a value outside the CHECK succeeded", name)
		}
	}
	f, err := db.UpsertFinding(t.Context(), model.Finding{Kind: model.KindJobFailure, Fingerprint: "fp", LastChangedAt: t0})
	if err != nil {
		t.Fatalf("UpsertFinding() error = %v", err)
	}
	if _, err := db.pool.ExecContext(t.Context(), "UPDATE finding_state SET state = 'archived' WHERE finding_id = ?", f.ID); err == nil {
		t.Errorf("finding_state.state: update to a value outside the CHECK succeeded")
	}
}

func TestSchema_DepVersionDeleteBehaviour(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repoID, packageID, advisoryID := seedAdvisoryParents(t, db)
	exec(t, db, `INSERT INTO manifests (id, repo_id, path, created_ts, updated_ts) VALUES (3, ?, 'go.mod', ?, ?)`, repoID, ts0, ts0)
	exec(t, db, `INSERT INTO commits (id, repo_id, sha, created_ts, updated_ts) VALUES (4, ?, 'abc123', ?, ?)`, repoID, ts0, ts0)
	exec(t, db, `INSERT INTO dep_versions (id, repo_id, package_id, manifest_id, version, from_commit_id, created_ts, updated_ts)
		VALUES (5, ?, ?, 3, 'v1.2.0', 4, ?, ?)`, repoID, packageID, ts0, ts0)
	exec(t, db, `INSERT INTO advisory_matches (repo_id, dep_version_id, advisory_id, created_ts, updated_ts)
		VALUES (?, 5, ?, ?, ?)`, repoID, advisoryID, ts0, ts0)
	exec(t, db, `INSERT INTO upstream_repos (id, url, created_ts, updated_ts)
		VALUES (6, 'https://github.com/example-org/example-lib', ?, ?)`, ts0, ts0)
	exec(t, db, `INSERT INTO upstream_issues (id, upstream_repo_id, number, created_ts, updated_ts) VALUES (7, 6, 42, ?, ?)`, ts0, ts0)
	exec(t, db, `INSERT INTO issue_candidates (issue_id, repo_id, package_id, dep_version_id, issue_body_hash, created_ts, updated_ts)
		VALUES (7, ?, ?, 5, 'hash', ?, ?)`, repoID, packageID, ts0, ts0)
	f, err := db.UpsertFinding(ctx, model.Finding{
		Kind: model.KindAdvisory, RepoID: repoID, PackageID: packageID, DepVersionID: 5,
		AdvisoryID: advisoryID, Fingerprint: "advisory|1|2|GO-2026-0001", LastChangedAt: t0,
	})
	if err != nil {
		t.Fatalf("UpsertFinding() error = %v", err)
	}

	exec(t, db, "DELETE FROM dep_versions WHERE id = 5")

	if got := count(t, db, "SELECT COUNT(*) FROM advisory_matches"); got != 0 {
		t.Errorf("advisory_matches rows after interval delete = %d, want 0 (cascade)", got)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM issue_candidates"); got != 0 {
		t.Errorf("issue_candidates rows after interval delete = %d, want 0 (cascade)", got)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM findings WHERE id = ? AND dep_version_id IS NULL", f.ID); got != 1 {
		t.Errorf("finding with NULL dep_version_id after interval delete: %d rows, want 1 (set null)", got)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM finding_state WHERE finding_id = ?", f.ID); got != 1 {
		t.Errorf("finding_state rows after interval delete = %d, want 1", got)
	}

	exec(t, db, `INSERT INTO dep_versions (id, repo_id, package_id, manifest_id, version, from_commit_id, created_ts, updated_ts)
		VALUES (8, ?, ?, 3, 'v1.3.0', 4, ?, ?)`, repoID, packageID, ts0, ts0)

	// The next match run upserts the same fingerprint against the recreated
	// interval, which relinks the finding without touching its history.
	exec(t, db, `UPDATE finding_state SET state = 'acked', changed_by = 'operator' WHERE finding_id = ?`, f.ID)
	relinked, err := db.UpsertFinding(ctx, model.Finding{
		Kind: model.KindAdvisory, RepoID: repoID, PackageID: packageID, DepVersionID: 8,
		AdvisoryID: advisoryID, Fingerprint: "advisory|1|2|GO-2026-0001", LastChangedAt: t0.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("re-upsert UpsertFinding() error = %v", err)
	}
	if relinked.ID != f.ID || relinked.DepVersionID != 8 || !relinked.FirstSeenAt.Equal(f.FirstSeenAt) ||
		relinked.State.State != model.StateAcked {
		t.Errorf("re-upsert after interval recreation = %+v, want ID %d, dep_version 8, first seen %v, state acked",
			relinked, f.ID, f.FirstSeenAt)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM findings WHERE id = ? AND dep_version_id = 8", f.ID); got != 1 {
		t.Errorf("finding linked to recreated interval: %d rows, want 1", got)
	}

	if _, err := db.pool.ExecContext(ctx, "DELETE FROM commits WHERE id = 4"); err == nil {
		t.Errorf("delete of a commit an interval references succeeded, want a foreign-key error")
	}
}

func TestInTx_CommitRollbackPanic(t *testing.T) {
	t.Parallel()

	t.Run("commit", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		err := db.InTx(t.Context(), func(tx *DB) error {
			_, err := tx.StartRun(t.Context(), "ingest", t0)
			return err
		})
		if err != nil {
			t.Fatalf("InTx() error = %v", err)
		}
		if got := count(t, db, "SELECT COUNT(*) FROM runs"); got != 1 {
			t.Errorf("runs after commit = %d, want 1", got)
		}
	})

	t.Run("rollback on error", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		boom := errors.New("boom")
		err := db.InTx(t.Context(), func(tx *DB) error {
			if _, err := tx.StartRun(t.Context(), "ingest", t0); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("InTx() error = %v, want boom", err)
		}
		if got := count(t, db, "SELECT COUNT(*) FROM runs"); got != 0 {
			t.Errorf("runs after rollback = %d, want 0", got)
		}
	})

	t.Run("rollback on panic", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			_ = db.InTx(t.Context(), func(tx *DB) error {
				if _, err := tx.StartRun(t.Context(), "ingest", t0); err != nil {
					return err
				}
				panic("kaboom")
			})
		}()
		if recovered != "kaboom" {
			t.Fatalf("recovered = %v, want the panic value kaboom", recovered)
		}
		if got := count(t, db, "SELECT COUNT(*) FROM runs"); got != 0 {
			t.Errorf("runs after panic = %d, want 0", got)
		}
		// The connection went back to the pool usable.
		if _, err := db.StartRun(t.Context(), "ingest", t0); err != nil {
			t.Errorf("StartRun() after panic error = %v", err)
		}
	})
}

func TestInTx_Nested(t *testing.T) {
	t.Parallel()

	t.Run("inner joins outer and commits with it", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		err := db.InTx(t.Context(), func(outer *DB) error {
			if _, err := outer.StartRun(t.Context(), "ingest", t0); err != nil {
				return err
			}
			return outer.InTx(t.Context(), func(inner *DB) error {
				if inner.q != outer.q {
					t.Errorf("inner InTx opened a new transaction")
				}
				_, err := inner.StartRun(t.Context(), "timeline", t0)
				return err
			})
		})
		if err != nil {
			t.Fatalf("InTx() error = %v", err)
		}
		if got := count(t, db, "SELECT COUNT(*) FROM runs"); got != 2 {
			t.Errorf("runs = %d, want 2", got)
		}
	})

	t.Run("outer error rolls back inner writes", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		boom := errors.New("boom")
		err := db.InTx(t.Context(), func(outer *DB) error {
			if err := outer.InTx(t.Context(), func(inner *DB) error {
				_, err := inner.StartRun(t.Context(), "timeline", t0)
				return err
			}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("InTx() error = %v, want boom", err)
		}
		if got := count(t, db, "SELECT COUNT(*) FROM runs"); got != 0 {
			t.Errorf("runs = %d, want 0", got)
		}
	})

	t.Run("inner error rolls back everything", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		boom := errors.New("boom")
		err := db.InTx(t.Context(), func(outer *DB) error {
			if _, err := outer.StartRun(t.Context(), "ingest", t0); err != nil {
				return err
			}
			return outer.InTx(t.Context(), func(*DB) error { return boom })
		})
		if !errors.Is(err, boom) {
			t.Fatalf("InTx() error = %v, want boom", err)
		}
		if got := count(t, db, "SELECT COUNT(*) FROM runs"); got != 0 {
			t.Errorf("runs = %d, want 0", got)
		}
	})
}

// holdQueue starts an InTx on db that writes a run for job, signals on the
// returned entered channel and then holds the write queue until release is
// closed. The InTx result arrives on done.
func holdQueue(t *testing.T, db *DB, job string, release <-chan struct{}) (entered <-chan struct{}, done <-chan error) {
	t.Helper()
	in := make(chan struct{})
	out := make(chan error, 1)
	go func() {
		out <- db.InTx(t.Context(), func(tx *DB) error {
			if _, err := tx.StartRun(t.Context(), job, t0); err != nil {
				return err
			}
			close(in)
			<-release
			return nil
		})
	}()
	return in, out
}

// checkQueueWaitErr fails the test unless err wraps target and came from
// InTx giving up its wait for the write queue, not from BeginTx: a caller
// that skipped the queue fails in BEGIN with the same context error but
// without the queue in its message.
func checkQueueWaitErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("queued InTx error = %v, want one wrapping %v", err, target)
	}
	if err == nil || !strings.Contains(err.Error(), "wait for write queue") {
		t.Errorf("queued InTx error = %v, want it to come from the write queue wait", err)
	}
}

func TestDB_InTx_ContextCancelledWhileQueued(t *testing.T) {
	t.Parallel()

	t.Run("cancelled after queueing", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			db := newTestDB(t)
			release := make(chan struct{})
			entered, firstDone := holdQueue(t, db, "first", release)
			<-entered

			ctx, cancel := context.WithCancel(t.Context())
			var ran bool
			secondDone := make(chan error, 1)
			go func() {
				secondDone <- db.InTx(ctx, func(tx *DB) error {
					ran = true
					_, err := tx.StartRun(ctx, "second", t0)
					return err
				})
			}()
			synctest.Wait() // the second InTx is parked on the queue
			cancel()

			checkQueueWaitErr(t, <-secondDone, context.Canceled)
			if ran {
				t.Errorf("queued InTx ran its function after its context was cancelled")
			}

			close(release)
			if err := <-firstDone; err != nil {
				t.Fatalf("holding InTx error = %v", err)
			}
			if got := count(t, db, "SELECT COUNT(*) FROM runs WHERE job = 'first'"); got != 1 {
				t.Errorf("first runs after release = %d, want 1", got)
			}
			if got := count(t, db, "SELECT COUNT(*) FROM runs WHERE job = 'second'"); got != 0 {
				t.Errorf("second runs = %d, want 0", got)
			}
			// The queue is free again.
			if err := db.InTx(t.Context(), func(tx *DB) error {
				_, err := tx.StartRun(t.Context(), "third", t0)
				return err
			}); err != nil {
				t.Errorf("InTx after release error = %v", err)
			}
		})
	})

	t.Run("already cancelled while queue is held", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		release := make(chan struct{})
		entered, firstDone := holdQueue(t, db, "first", release)
		<-entered

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		// The queue is held, so the only ready case is the cancelled context.
		start := time.Now()
		err := db.InTx(ctx, func(*DB) error {
			t.Errorf("InTx ran its function with a cancelled context")
			return nil
		})
		// Giving up the queue wait is immediate; a caller that reached
		// BEGIN IMMEDIATE could sit in the 5 s busy handler.
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("InTx with a cancelled context returned after %v, want well under the busy timeout", elapsed)
		}
		checkQueueWaitErr(t, err, context.Canceled)

		close(release)
		if err := <-firstDone; err != nil {
			t.Fatalf("holding InTx error = %v", err)
		}
	})

	t.Run("deadline exceeded", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			db := newTestDB(t)
			release := make(chan struct{})
			entered, firstDone := holdQueue(t, db, "first", release)
			<-entered

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			err := db.InTx(ctx, func(*DB) error {
				t.Errorf("InTx ran its function after its deadline")
				return nil
			})
			checkQueueWaitErr(t, err, context.DeadlineExceeded)

			close(release)
			if err := <-firstDone; err != nil {
				t.Fatalf("holding InTx error = %v", err)
			}
		})
	})
}

func TestDB_InTx_QueueServesArrivalOrder(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := newTestDB(t)
		release := make(chan struct{})
		entered, firstDone := holdQueue(t, db, "holder", release)
		<-entered

		const waiters = 5
		var (
			mu    sync.Mutex
			order []int
			wg    sync.WaitGroup
		)
		for i := range waiters {
			wg.Go(func() {
				err := db.InTx(t.Context(), func(tx *DB) error {
					mu.Lock()
					order = append(order, i)
					mu.Unlock()
					_, err := tx.StartRun(t.Context(), fmt.Sprintf("waiter-%d", i), t0)
					return err
				})
				if err != nil {
					t.Errorf("waiter %d InTx error = %v", i, err)
				}
			})
			synctest.Wait() // waiter i is parked before waiter i+1 arrives
		}
		close(release)
		if err := <-firstDone; err != nil {
			t.Fatalf("holding InTx error = %v", err)
		}
		wg.Wait()

		want := []int{0, 1, 2, 3, 4}
		if !slices.Equal(order, want) {
			t.Errorf("waiters served in order %v, want %v", order, want)
		}
	})
}

func TestDB_InTx_QueueReleasedAfterErrorAndPanic(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	boom := errors.New("boom")
	if err := db.InTx(t.Context(), func(*DB) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("InTx() error = %v, want boom", err)
	}
	func() {
		defer func() {
			if p := recover(); p != "kaboom" {
				t.Errorf("recovered = %v, want kaboom", p)
			}
		}()
		_ = db.InTx(t.Context(), func(*DB) error { panic("kaboom") })
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := db.InTx(ctx, func(tx *DB) error {
		_, err := tx.StartRun(ctx, "after", t0)
		return err
	}); err != nil {
		t.Errorf("InTx() after an error and a panic error = %v, want the queue free", err)
	}
}

// runStarter is a consumer-declared interface, as a service would declare it.
type runStarter interface {
	StartRun(ctx context.Context, job string, startedAt time.Time) (model.Run, error)
}

func TestBind(t *testing.T) {
	t.Parallel()

	t.Run("commits", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		inTx := Bind(db, func(d *DB) runStarter { return d })
		err := inTx(t.Context(), func(s runStarter) error {
			if d, ok := s.(*DB); !ok || !d.inTx {
				t.Errorf("Bind passed a store outside the transaction")
			}
			_, err := s.StartRun(t.Context(), "ingest", t0)
			return err
		})
		if err != nil {
			t.Fatalf("bound InTx error = %v", err)
		}
		if got := count(t, db, "SELECT COUNT(*) FROM runs"); got != 1 {
			t.Errorf("runs = %d, want 1", got)
		}
	})

	t.Run("rolls back on error", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		boom := errors.New("boom")
		inTx := Bind(db, func(d *DB) runStarter { return d })
		err := inTx(t.Context(), func(s runStarter) error {
			if _, err := s.StartRun(t.Context(), "ingest", t0); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("bound InTx error = %v, want boom", err)
		}
		if got := count(t, db, "SELECT COUNT(*) FROM runs"); got != 0 {
			t.Errorf("runs = %d, want 0", got)
		}
	})
}

func TestTimestamps_RoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("format and parse", func(t *testing.T) {
		t.Parallel()
		zone := time.FixedZone("UTC+5:30", 5*3600+1800)
		in := time.Date(2026, 3, 1, 17, 30, 5, 987654321, zone)
		stored := formatTime(in)
		if stored != "2026-03-01T12:00:05Z" {
			t.Errorf("formatTime(%v) = %v, want 2026-03-01T12:00:05Z", in, stored)
		}
		got, err := parseTime(sql.NullString{String: stored.(string), Valid: true})
		if err != nil {
			t.Fatalf("parseTime() error = %v", err)
		}
		want := time.Date(2026, 3, 1, 12, 0, 5, 0, time.UTC)
		if !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("parseTime(%v) = %v, want %v in UTC", stored, got, want)
		}

		if v := formatTime(time.Time{}); v != nil {
			t.Errorf("formatTime(zero) = %v, want nil", v)
		}
		zero, err := parseTime(sql.NullString{})
		if err != nil || !zero.IsZero() {
			t.Errorf("parseTime(NULL) = %v, %v; want zero time, nil", zero, err)
		}
		if _, err := parseTime(sql.NullString{String: "2026-03-01 12:00:00", Valid: true}); err == nil {
			t.Errorf("parseTime(space-separated) error = nil, want an error")
		}
	})

	t.Run("through the database", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		zone := time.FixedZone("UTC-7", -7*3600)
		in := time.Date(2026, 3, 1, 5, 0, 5, 123456789, zone)
		if _, err := db.StartRun(t.Context(), "ingest", in); err != nil {
			t.Fatalf("StartRun() error = %v", err)
		}
		var raw string
		var finished sql.NullString
		if err := db.pool.QueryRowContext(t.Context(), "SELECT started_ts, finished_ts FROM runs").Scan(&raw, &finished); err != nil {
			t.Fatalf("read runs: %v", err)
		}
		if raw != "2026-03-01T12:00:05Z" {
			t.Errorf("stored started_ts = %q, want 2026-03-01T12:00:05Z", raw)
		}
		if finished.Valid {
			t.Errorf("stored finished_ts = %q, want NULL for the zero time", finished.String)
		}
		runs, err := db.LatestRuns(t.Context())
		if err != nil || len(runs) != 1 {
			t.Fatalf("LatestRuns() = %v, %v; want one run", runs, err)
		}
		want := time.Date(2026, 3, 1, 12, 0, 5, 0, time.UTC)
		if got := runs[0].StartedAt; !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("StartedAt = %v, want %v in UTC", got, want)
		}
		if !runs[0].FinishedAt.IsZero() {
			t.Errorf("FinishedAt = %v, want zero", runs[0].FinishedAt)
		}
	})
}

func TestDB_ClosedReturnsErrors(t *testing.T) {
	t.Parallel()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "upw.sqlite"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	ctx := t.Context()
	for name, call := range map[string]func() error{
		"Migrate":  func() error { return db.Migrate(ctx) },
		"StartRun": func() error { _, err := db.StartRun(ctx, "ingest", t0); return err },
		"FinishRun": func() error {
			return db.FinishRun(ctx, model.Run{ID: 1, Status: model.RunSucceeded, FinishedAt: t0})
		},
		"LatestRuns": func() error { _, err := db.LatestRuns(ctx); return err },
		"UpsertFinding": func() error {
			_, err := db.UpsertFinding(ctx, model.Finding{Kind: model.KindJobFailure, Fingerprint: "fp", LastChangedAt: t0})
			return err
		},
		"ResolveFinding":   func() error { return db.ResolveFinding(ctx, "fp", t0) },
		"ListOpenFindings": func() error { _, err := db.ListOpenFindings(ctx); return err },
		"ListFindings": func() error {
			_, err := db.ListFindings(ctx, model.FindingFilter{Now: t0})
			return err
		},
		"ListUnresolvedFindings": func() error { _, err := db.ListUnresolvedFindings(ctx, model.KindAdvisory); return err },
		"GetFinding":             func() error { _, err := db.GetFinding(ctx, 1); return err },
		"SaveFindingState": func() error {
			return db.SaveFindingState(ctx, 1, model.FindingState{State: model.StateAcked})
		},
		"MarkReported": func() error { return db.MarkReported(ctx, []int64{1}, t0) },
		"SaveDigest": func() error {
			_, err := db.SaveDigest(ctx, model.DigestRecord{Channel: "stdout", Status: model.DigestSent, SentAt: t0})
			return err
		},
		"FailedDigests":     func() error { _, err := db.FailedDigests(ctx); return err },
		"MarkDigestRetried": func() error { return db.MarkDigestRetried(ctx, 1) },
		"LastSentDigest":    func() error { _, err := db.LastSentDigest(ctx); return err },
		"BackupTo":          func() error { return db.BackupTo(ctx, filepath.Join(t.TempDir(), "backup.sqlite")) },
		"InTx":              func() error { return db.InTx(ctx, func(*DB) error { return nil }) },
	} {
		if err := call(); err == nil {
			t.Errorf("%s after Close() error = nil, want an error", name)
		}
	}
}
