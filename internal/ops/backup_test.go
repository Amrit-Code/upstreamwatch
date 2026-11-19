package ops_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/ops"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

var _ ops.Backuper = (*store.DB)(nil)

// day returns noon on 2026-03-<n> in the local zone, so the backup's file
// name is upw-202603<nn>.sqlite on any machine.
func day(n int) time.Time { return time.Date(2026, 3, n, 12, 0, 0, 0, time.Local) }

// fakeBackuper writes a small file at each path it is asked for, or fails.
type fakeBackuper struct {
	err   error
	paths []string
}

func (f *fakeBackuper) BackupTo(ctx context.Context, path string) error {
	f.paths = append(f.paths, path)
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(path, []byte("backup"), 0o600)
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// listDir returns the names in dir, sorted.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestBackup_Name(t *testing.T) {
	t.Parallel()
	if got := ops.NewBackup(&fakeBackuper{}, ops.BackupOptions{}, discard()).Name(); got != "backup" {
		t.Errorf("Name() = %q, want %q", got, "backup")
	}
}

func TestBackup_EightDays(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}
	now := day(1)
	db := &fakeBackuper{}
	b := ops.NewBackup(db, ops.BackupOptions{Dir: dir, Keep: 7, Now: func() time.Time { return now }}, discard())

	for i := 1; i <= 8; i++ {
		now = day(i)
		stats, err := b.Run(t.Context(), model.Scope{})
		if err != nil {
			t.Fatalf("run %d: Run() error = %v", i, err)
		}
		wantDeleted := int64(0)
		if i == 8 {
			wantDeleted = 1
		}
		want := model.Stats{"exists": 0, "written": 1, "deleted": wantDeleted}
		if !reflect.DeepEqual(stats, want) {
			t.Errorf("run %d: Run() stats = %v, want %v", i, stats, want)
		}
	}

	want := []string{
		"notes.txt",
		"upw-20260302.sqlite", "upw-20260303.sqlite", "upw-20260304.sqlite", "upw-20260305.sqlite",
		"upw-20260306.sqlite", "upw-20260307.sqlite", "upw-20260308.sqlite",
	}
	if got := listDir(t, dir); !slices.Equal(got, want) {
		t.Errorf("directory after eight runs = %v, want %v", got, want)
	}
	if got, err := os.ReadFile(notes); err != nil || string(got) != "keep me" {
		t.Errorf("notes.txt = %q, %v; want it untouched", got, err)
	}
}

func TestBackup_SameDayTwice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db := &fakeBackuper{}
	b := ops.NewBackup(db, ops.BackupOptions{Dir: dir, Now: func() time.Time { return day(5) }}, discard())

	if _, err := b.Run(t.Context(), model.Scope{}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	stats, err := b.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	want := model.Stats{"exists": 1, "written": 0, "deleted": 0}
	if !reflect.DeepEqual(stats, want) {
		t.Errorf("second Run() stats = %v, want %v", stats, want)
	}
	if len(db.paths) != 1 {
		t.Errorf("BackupTo called %d times, want 1: %v", len(db.paths), db.paths)
	}
}

func TestBackup_ExistingStillPrunes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, n := range []string{"upw-20260101.sqlite", "upw-20260102.sqlite", "upw-20260305.sqlite"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	// Files and directories that only look like backups are never deleted.
	if err := os.Mkdir(filepath.Join(dir, "upw-20250101.sqlite"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "upw-2025010.sqlite"), nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	db := &fakeBackuper{}
	b := ops.NewBackup(db, ops.BackupOptions{Dir: dir, Keep: 1, Now: func() time.Time { return day(5) }}, discard())

	stats, err := b.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := model.Stats{"exists": 1, "written": 0, "deleted": 2}
	if !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
	wantNames := []string{"upw-2025010.sqlite", "upw-20250101.sqlite", "upw-20260305.sqlite"}
	if got := listDir(t, dir); !slices.Equal(got, wantNames) {
		t.Errorf("directory = %v, want %v", got, wantNames)
	}
	if len(db.paths) != 0 {
		t.Errorf("BackupTo called with %v, want no call", db.paths)
	}
}

func TestBackup_DefaultKeepAndDirMode(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "backups")
	now := day(1)
	b := ops.NewBackup(&fakeBackuper{}, ops.BackupOptions{Dir: dir, Now: func() time.Time { return now }}, discard())

	for i := 1; i <= 9; i++ {
		now = day(i)
		if _, err := b.Run(t.Context(), model.Scope{}); err != nil {
			t.Fatalf("run %d: Run() error = %v", i, err)
		}
	}
	if got := len(listDir(t, dir)); got != 7 {
		t.Errorf("%d backups kept, want the default 7", got)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("directory mode = %v, want 0700", got)
	}
}

func TestBackup_BackupFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	errDisk := errors.New("disk full")
	b := ops.NewBackup(&fakeBackuper{err: errDisk}, ops.BackupOptions{Dir: dir, Now: func() time.Time { return day(1) }}, discard())

	stats, err := b.Run(t.Context(), model.Scope{})
	if !errors.Is(err, errDisk) {
		t.Errorf("Run() error = %v, want %v", err, errDisk)
	}
	if stats["written"] != 0 {
		t.Errorf("Run() written = %d, want 0", stats["written"])
	}
}

func TestBackup_DeleteFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, n := range []string{"upw-20260101.sqlite", "upw-20260102.sqlite", "upw-20260305.sqlite"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	// A directory without write permission refuses every removal.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restore mode: %v", err)
		}
	})
	b := ops.NewBackup(&fakeBackuper{}, ops.BackupOptions{Dir: dir, Keep: 1, Now: func() time.Time { return day(5) }}, discard())

	stats, err := b.Run(t.Context(), model.Scope{})
	if err == nil {
		t.Fatal("Run() error = nil, want the deletion errors")
	}
	if _, ok := errors.AsType[*os.PathError](err); !ok {
		t.Errorf("Run() error = %v, want a path error from the removal", err)
	}
	if stats["deleted"] != 0 || stats["exists"] != 1 {
		t.Errorf("Run() stats = %v, want exists 1 and deleted 0", stats)
	}
	if got := len(listDir(t, dir)); got != 3 {
		t.Errorf("%d files left, want all 3", got)
	}
}

func TestBackup_RealStore(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tmp := t.TempDir()
	db, err := store.Open(ctx, filepath.Join(tmp, "upw.sqlite"), discard())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if _, err := db.UpsertRepo(ctx, "example-org/example-server", "https://example.com/example-org/example-server.git"); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}

	dir := filepath.Join(tmp, "backups")
	b := ops.NewBackup(db, ops.BackupOptions{Dir: dir, Now: func() time.Time { return day(1) }}, discard())
	if _, err := b.Run(ctx, model.Scope{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	copyDB, err := store.Open(ctx, filepath.Join(dir, "upw-20260301.sqlite"), discard())
	if err != nil {
		t.Fatalf("Open(backup) error = %v", err)
	}
	t.Cleanup(func() {
		if err := copyDB.Close(); err != nil {
			t.Errorf("Close(backup) error = %v", err)
		}
	})
	got, err := copyDB.GetRepo(ctx, "example-org/example-server")
	if err != nil {
		t.Fatalf("backup GetRepo() error = %v", err)
	}
	if got.URL != "https://example.com/example-org/example-server.git" {
		t.Errorf("backup GetRepo() URL = %q, want the inserted URL", got.URL)
	}
}
