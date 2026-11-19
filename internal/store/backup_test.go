package store

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestBackupTo(t *testing.T) {
	t.Parallel()

	t.Run("copy has the rows and mode 0600", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		ctx := t.Context()
		if _, err := db.StartRun(ctx, "ingest", t0); err != nil {
			t.Fatalf("StartRun() error = %v", err)
		}
		f, err := db.UpsertFinding(ctx, model.Finding{Kind: model.KindJobFailure, Fingerprint: "job_failure|ingest", LastChangedAt: t0})
		if err != nil {
			t.Fatalf("UpsertFinding() error = %v", err)
		}

		// The parent directory does not exist yet.
		dir := filepath.Join(t.TempDir(), "backups")
		path := filepath.Join(dir, "upw-20260301.sqlite")
		if err := db.BackupTo(ctx, path); err != nil {
			t.Fatalf("BackupTo() error = %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat backup: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("backup mode = %v, want 0600", got)
		}
		dirInfo, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat backup directory: %v", err)
		}
		if got := dirInfo.Mode().Perm(); got != 0o700 {
			t.Errorf("backup directory mode = %v, want 0700", got)
		}

		copyDB, err := Open(ctx, path, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("Open(backup) error = %v", err)
		}
		t.Cleanup(func() {
			if err := copyDB.Close(); err != nil {
				t.Errorf("Close(backup) error = %v", err)
			}
		})
		if err := copyDB.Migrate(ctx); err != nil {
			t.Errorf("Migrate(backup) error = %v, want the copy at the current schema", err)
		}
		runs, err := copyDB.LatestRuns(ctx)
		if err != nil || len(runs) != 1 || runs[0].Job != "ingest" {
			t.Errorf("backup LatestRuns() = %+v, %v; want the ingest run", runs, err)
		}
		got, err := copyDB.GetFinding(ctx, f.ID)
		if err != nil || got.Fingerprint != f.Fingerprint {
			t.Errorf("backup GetFinding(%d) = %+v, %v; want %s", f.ID, got, err, f.Fingerprint)
		}
	})

	t.Run("existing path is refused and left alone", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		path := filepath.Join(t.TempDir(), "upw.sqlite.bak")
		const content = "not a database"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write existing file: %v", err)
		}
		if err := db.BackupTo(t.Context(), path); err == nil {
			t.Fatal("BackupTo(existing path) error = nil, want an error")
		}
		b, err := os.ReadFile(path)
		if err != nil || string(b) != content {
			t.Errorf("existing file after BackupTo = %q, %v; want %q unchanged", b, err, content)
		}
	})

	t.Run("refused inside a transaction", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		path := filepath.Join(t.TempDir(), "upw.sqlite.bak")
		err := db.InTx(t.Context(), func(tx *DB) error { return tx.BackupTo(t.Context(), path) })
		if err == nil {
			t.Fatal("BackupTo() inside InTx error = nil, want an error")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("stat backup after refusal error = %v, want not exist", err)
		}
	})

	t.Run("URI and query paths are refused", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		for _, path := range []string{"", "file:upw.sqlite.bak", filepath.Join(t.TempDir(), "upw.sqlite?mode=memory")} {
			if err := db.BackupTo(t.Context(), path); err == nil {
				t.Errorf("BackupTo(%q) error = nil, want an error", path)
			}
		}
	})
}
