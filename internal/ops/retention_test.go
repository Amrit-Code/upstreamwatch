package ops_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/ops"
)

// TestBackup_RetentionOverEightDays runs the job once a day for eight days
// and checks, after every run, which backups are left: the newest Keep, with
// files that are not backups never touched.
func TestBackup_RetentionOverEightDays(t *testing.T) {
	t.Parallel()
	for _, keep := range []int{7, 1} {
		t.Run(fmt.Sprintf("keep %d", keep), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			others := []string{"notes.txt", "upw-latest.sqlite", "upw-20260301.sqlite.tmp"}
			for _, name := range others {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			now := day(1)
			db := &fakeBackuper{}
			b := ops.NewBackup(db, ops.BackupOptions{Dir: dir, Keep: keep, Now: func() time.Time { return now }}, discard())

			for i := 1; i <= 8; i++ {
				now = day(i)
				stats, err := b.Run(t.Context(), model.Scope{})
				if err != nil {
					t.Fatalf("day %d: Run() error = %v", i, err)
				}
				// Expected by counting: before the run i-1 backups (capped at
				// keep), one is written, the excess over keep deleted.
				before := min(i-1, keep)
				wantDeleted := int64(max(0, before+1-keep))
				want := model.Stats{"exists": 0, "written": 1, "deleted": wantDeleted}
				if !reflect.DeepEqual(stats, want) {
					t.Errorf("day %d: Run() stats = %v, want %v", i, stats, want)
				}
				var wantNames []string
				for d := max(1, i-keep+1); d <= i; d++ {
					wantNames = append(wantNames, fmt.Sprintf("upw-202603%02d.sqlite", d))
				}
				wantNames = append(wantNames, others...)
				slices.Sort(wantNames)
				if got := listDir(t, dir); !slices.Equal(got, wantNames) {
					t.Errorf("day %d: directory = %v, want %v", i, got, wantNames)
				}
			}
			if len(db.paths) != 8 {
				t.Errorf("BackupTo called %d times, want 8", len(db.paths))
			}
			for _, name := range others {
				if got, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(got) != name {
					t.Errorf("%s = %q, %v; want it untouched", name, got, err)
				}
			}
		})
	}
}
