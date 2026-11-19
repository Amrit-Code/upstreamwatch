package gitsrc

import (
	"errors"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo"
)

// syncedClone syncs a one-commit fixture into a fresh bare clone and returns
// the CLI, the clone directory and its head.
func syncedClone(t *testing.T) (*CLI, string, string) {
	t.Helper()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	head := up.Commit("first", time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	root := t.TempDir()
	g := New(Options{
		ReposDir:      filepath.Join(root, "repos"),
		HomeDir:       filepath.Join(root, "home"),
		AllowFileURLs: true,
	}, slog.New(slog.DiscardHandler))
	res, err := g.Sync(t.Context(), model.TargetRepo{Slug: "app", URL: up.URL(), Branch: "main"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	return g, res.ClonePath, head
}

// litter leaves what an interrupted, rejected fetch can leave in the clone at
// dir: an unreferenced loose object and a temporary pack file, about 1 MiB
// each.
func litter(t *testing.T, dir string) {
	t.Helper()
	data := make([]byte, 1<<20)
	rand.NewChaCha8([32]byte{7}).Read(data) // incompressible
	src := filepath.Join(t.TempDir(), "orphan.bin")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	gitrepo.Git(t, dir, "hash-object", "-w", "--", src)
	if err := os.WriteFile(filepath.Join(dir, "objects", "pack", "tmp_pack_rejected"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestShrinkAfterCap_PrunesAndKeepsClone(t *testing.T) {
	t.Parallel()
	g, dir, head := syncedClone(t)
	before, err := dirSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	litter(t, dir)
	g.opts.MaxCloneBytes = before + 256<<10

	capErr := g.capError()
	err = g.shrinkAfterCap(t.Context(), dir, capErr)
	if err != capErr {
		t.Errorf("shrinkAfterCap() = %v, want exactly the cap error %v", err, capErr)
	}
	after, sizeErr := dirSize(dir)
	if sizeErr != nil {
		t.Fatalf("clone gone or unreadable after a prune that fit the cap: %v", sizeErr)
	}
	if after > before+64<<10 {
		t.Errorf("clone size after prune = %d, want at most %d (before %d plus slack)", after, before+64<<10, before)
	}
	if got := gitrepo.Git(t, dir, "rev-parse", "refs/heads/main"); got != head {
		t.Errorf("main after shrinkAfterCap = %s, want %s", got, head)
	}
}

func TestShrinkAfterCap_RemovesCloneStillOverCap(t *testing.T) {
	t.Parallel()
	g, dir, _ := syncedClone(t)
	litter(t, dir)
	g.opts.MaxCloneBytes = 1 << 10 // below what the clone needs even after the prune

	capErr := g.capError()
	err := g.shrinkAfterCap(t.Context(), dir, capErr)
	if !errors.Is(err, capErr) {
		t.Errorf("shrinkAfterCap() = %v, want it to wrap the cap error", err)
	}
	if err == nil || !strings.Contains(err.Error(), "clone removed") {
		t.Errorf("shrinkAfterCap() = %v, want it to say the clone was removed", err)
	}
	if _, statErr := os.Stat(dir); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("clone directory still exists (stat error %v)", statErr)
	}
}

func TestCapError_Message(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cap  int64
		want string
	}{
		{256 << 10, "clone exceeded the 0.25 MiB size cap"},
		{2048 << 20, "clone exceeded the 2048 MiB size cap"},
	}
	for _, tt := range tests {
		g := New(Options{MaxCloneBytes: tt.cap}, slog.New(slog.DiscardHandler))
		err := g.capError()
		if got := err.Error(); got != tt.want {
			t.Errorf("capError(%d) = %q, want %q", tt.cap, got, tt.want)
		}
		if _, ok := errors.AsType[*capExceededError](err); !ok {
			t.Errorf("capError(%d) is %T, want *capExceededError", tt.cap, err)
		}
	}
}
