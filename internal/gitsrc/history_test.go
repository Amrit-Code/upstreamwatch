package gitsrc_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo"
)

// syncFixture syncs up into a fresh bare clone and returns the CLI and the
// target as the timeline job would see it.
func syncFixture(t *testing.T, up *gitrepo.Repo, opts gitsrc.Options) (*gitsrc.CLI, model.TargetRepo) {
	t.Helper()
	g := gitsrc.New(opts, discard())
	repo := model.TargetRepo{Slug: "fixture", URL: up.URL(), Branch: "main"}
	res, err := g.Sync(t.Context(), repo)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	repo.ClonePath = res.ClonePath
	repo.DefaultBranch = res.Branch
	return g, repo
}

var manifestPatterns = []string{"**/go.mod", "**/requirements*.txt"}

func TestManifestCommits_FirstParent(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n\nrequire example.com/lib v1.0.0\n")
	up.Write("README", "app\n")
	base := up.Commit("add go.mod", day(1))
	up.Write("go.mod", "module example.com/app\n\nrequire example.com/lib v1.1.0\n")
	bump := up.Commit("bump lib on main", day(2))

	// A bump on a branch that is reverted before the merge: neither branch
	// commit nor the merge may appear.
	up.Branch("feature")
	up.Write("go.mod", "module example.com/app\n\nrequire example.com/lib v2.0.0\n")
	up.Commit("bump lib on feature", day(3))
	up.Write("go.mod", "module example.com/app\n\nrequire example.com/lib v1.1.0\n")
	up.Commit("revert lib on feature", day(4))
	up.Switch("main")
	up.Merge("feature", "merge feature", day(5))

	// A branch adding requirements.txt, merged with --no-ff after main moved
	// on: only the merge appears, at its own committer date.
	up.Branch("python")
	up.Write("requirements.txt", "requests==2.31.0\n")
	up.Commit("add requirements", day(6))
	up.Write("requirements.txt", "requests==2.32.0\n")
	up.Commit("bump requests", day(7))
	up.Switch("main")
	up.Write("README", "app, now with python\n")
	up.Commit("readme", day(8))
	merge := up.Merge("python", "merge python", day(9))

	g, repo := syncFixture(t, up, testOptions(t))
	got, err := g.ManifestCommits(t.Context(), repo, "", merge, manifestPatterns)
	if err != nil {
		t.Fatalf("ManifestCommits() error = %v", err)
	}
	want := []model.Commit{
		{SHA: base, CommittedAt: day(1), AuthorName: gitrepo.AuthorName, AuthorEmail: gitrepo.AuthorEmail, Subject: "add go.mod", Paths: []string{"go.mod"}},
		{SHA: bump, CommittedAt: day(2), AuthorName: gitrepo.AuthorName, AuthorEmail: gitrepo.AuthorEmail, Subject: "bump lib on main", Paths: []string{"go.mod"}},
		{SHA: merge, CommittedAt: day(9), AuthorName: gitrepo.AuthorName, AuthorEmail: gitrepo.AuthorEmail, Subject: "merge python", Paths: []string{"requirements.txt"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ManifestCommits() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestManifestCommits_FromAndDeletes(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	up.Write("svc/requirements-dev.txt", "pytest==8.0.0\n")
	first := up.Commit("add manifests", day(1))
	up.Write("svc/requirements-dev.txt", "pytest==8.1.0\n")
	second := up.Commit("bump pytest", day(2))
	up.Write("notes.txt", "no manifest here\n")
	up.Commit("notes", day(3))
	up.Delete("go.mod")
	up.Write("svc/requirements-dev.txt", "pytest==8.2.0\n")
	third := up.Commit("drop go.mod", day(4))

	g, repo := syncFixture(t, up, testOptions(t))

	got, err := g.ManifestCommits(t.Context(), repo, first, third, manifestPatterns)
	if err != nil {
		t.Fatalf("ManifestCommits(from) error = %v", err)
	}
	if len(got) != 2 || got[0].SHA != second || got[1].SHA != third {
		t.Fatalf("ManifestCommits(from %s) = %+v, want commits %s and %s", first, got, second, third)
	}
	if want := []string{"go.mod", "svc/requirements-dev.txt"}; !reflect.DeepEqual(got[1].Paths, want) {
		t.Errorf("deleting commit Paths = %q, want %q", got[1].Paths, want)
	}

	all, err := g.ManifestCommits(t.Context(), repo, "", third, manifestPatterns)
	if err != nil {
		t.Fatalf("ManifestCommits(all) error = %v", err)
	}
	if len(all) != 3 || all[0].SHA != first {
		t.Errorf("ManifestCommits(from root) = %+v, want 3 commits starting at %s", all, first)
	}

	none, err := g.ManifestCommits(t.Context(), repo, third, third, manifestPatterns)
	if err != nil || len(none) != 0 {
		t.Errorf("ManifestCommits(from = to) = %+v, %v, want no commits", none, err)
	}
}

func TestManifestCommits_Errors(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	head := up.Commit("first", day(1))
	g, repo := syncFixture(t, up, testOptions(t))

	tests := []struct {
		name     string
		repo     model.TargetRepo
		from, to string
		patterns []string
		want     string
	}{
		{"no patterns", repo, "", head, nil, "no patterns"},
		{"empty pattern", repo, "", head, []string{"go.mod", ""}, "empty pattern"},
		{"empty to", repo, "", "", manifestPatterns, "revision"},
		{"option as revision", repo, "--all", head, manifestPatterns, "revision"},
		{"unknown commit", repo, "", strings.Repeat("0", 40), manifestPatterns, "run git log"},
		{"missing clone", model.TargetRepo{Slug: "gone", ClonePath: t.TempDir() + "/nope.git"}, "", head, manifestPatterns, "clone missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := g.ManifestCommits(t.Context(), tt.repo, tt.from, tt.to, tt.patterns)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ManifestCommits() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// TestManifestCommits_LogCap runs ManifestCommits on a history whose log
// output is far larger than both MaxLogBytes and a pipe buffer, so git is
// still writing when the cap trips and must be killed.
func TestManifestCommits_LogCap(t *testing.T) {
	up := gitrepo.New(t, "main")
	var head string
	for c := range 3 {
		for i := range 1000 {
			up.Write(fmt.Sprintf("services/a-fairly-long-service-directory-name-%04d/go.mod", i),
				fmt.Sprintf("module example.com/svc%d\n\ngo 1.2%d\n", i, c))
		}
		head = up.Commit(fmt.Sprintf("bump %d", c), day(1+c))
	}
	opts := testOptions(t)
	g, repo := syncFixture(t, up, opts)

	// With the default cap the same history reads, so the cap below is what
	// fails the call.
	all, err := g.ManifestCommits(t.Context(), repo, "", head, manifestPatterns)
	if err != nil || len(all) != 3 || len(all[2].Paths) != 1000 {
		t.Fatalf("ManifestCommits(default cap) = %d commits, %v, want 3 commits of 1000 paths", len(all), err)
	}

	pidFile, _ := fakeGitPath(t)
	opts.MaxLogBytes = 16 << 10
	capped := gitsrc.New(opts, discard())
	goroutines := runtime.NumGoroutine()
	got, err := capped.ManifestCommits(t.Context(), repo, "", head, manifestPatterns)
	if err == nil || !strings.Contains(err.Error(), "git log output exceeded the 0.015625 MiB limit") {
		t.Fatalf("ManifestCommits(16 KiB cap) = %d commits, %v, want the log cap error", len(got), err)
	}
	if got != nil {
		t.Errorf("ManifestCommits(16 KiB cap) commits = %v on error, want nil", got)
	}

	// Every git process the call started has been waited for.
	pids, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read recorded pids: %v", err)
	}
	if len(strings.Fields(string(pids))) == 0 {
		t.Fatal("no git process recorded; the stand-in git was not used")
	}
	for _, field := range strings.Fields(string(pids)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatalf("recorded pid %q: %v", field, err)
		}
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Errorf("git process %d still exists after ManifestCommits returned (kill -0: %v)", pid, err)
		}
	}
	// The goroutines os/exec started have ended; one that is exiting may
	// still be counted for a moment, so yield before deciding.
	for range 1000 {
		if runtime.NumGoroutine() <= goroutines {
			break
		}
		runtime.Gosched()
	}
	if n := runtime.NumGoroutine(); n > goroutines {
		t.Errorf("goroutines after ManifestCommits = %d, want at most %d", n, goroutines)
	}
}

// TestIsAncestor_PrunedAfterForcePush force-moves the branch to an unrelated
// history, syncs, and drops the old commits from the clone, as git's gc
// would. The old head is then not an ancestor of the new one, without an
// error, so the timeline can rebuild instead of failing every run.
func TestIsAncestor_PrunedAfterForcePush(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	old := up.Commit("old history", day(1))
	g, repo := syncFixture(t, up, testOptions(t))
	oldFiles := objectFiles(t, repo.ClonePath)

	// The rewritten history shares no object with the old one, so the
	// later fetch brings every object it needs in files of its own.
	up.Git("checkout", "--quiet", "--orphan", "rewrite")
	up.Write("go.mod", "module example.com/rewritten\n")
	newHead := up.Commit("unrelated history", day(2))
	up.Git("branch", "-M", "rewrite", "main")

	res, err := g.Sync(t.Context(), model.TargetRepo{Slug: "fixture", URL: up.URL(), Branch: "main"})
	if err != nil || res.Head != newHead {
		t.Fatalf("Sync() after force-push = %+v, %v, want Head %s", res, err, newHead)
	}
	// Collecting the old history with git repack and prune left the old
	// commit in place on some git versions (seen on Linux CI), so the
	// fixture deletes the object files the first sync wrote instead. What
	// is under test is IsAncestor once the object is gone, not how git
	// collects it.
	for _, f := range oldFiles {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("remove old object file: %v", err)
		}
	}
	// Optional indexes that automatic maintenance may have written can
	// still name the old commit or the removed packs; git works without
	// them.
	for _, pattern := range []string{"objects/info/commit-graph*", "objects/pack/multi-pack-index*"} {
		matches, err := filepath.Glob(filepath.Join(repo.ClonePath, pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			if err := os.RemoveAll(m); err != nil {
				t.Fatalf("remove %s: %v", m, err)
			}
		}
	}
	if objects := gitrepo.Git(t, repo.ClonePath, "cat-file", "--batch-all-objects", "--batch-check"); strings.Contains(objects, old) || !strings.Contains(objects, newHead) {
		t.Fatalf("clone objects after removing the old history = \n%s\nwant %s gone and %s present", objects, old, newHead)
	}

	got, err := g.IsAncestor(t.Context(), repo, old, newHead)
	if err != nil || got {
		t.Errorf("IsAncestor(pruned old head, new head) = %v, %v, want false, nil", got, err)
	}
	if _, err := g.IsAncestor(t.Context(), repo, newHead, old); err == nil {
		t.Error("IsAncestor(new head, pruned commit) = nil error, want an error")
	}
	missing := repo
	missing.ClonePath = t.TempDir() + "/nope.git"
	if _, err := g.IsAncestor(t.Context(), missing, old, newHead); err == nil || !strings.Contains(err.Error(), "clone missing") {
		t.Errorf("IsAncestor(missing clone) error = %v, want clone missing", err)
	}
}

// objectFiles returns the pack and loose object files in the clone at dir.
func objectFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	objects := filepath.Join(dir, "objects")
	err := filepath.WalkDir(objects, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "info" {
			return fs.SkipDir
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("list object files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no object files found; the fixture would assert nothing")
	}
	return files
}

func TestHeadAndIsAncestor(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	first := up.Commit("first", day(1))
	second := up.Commit("second", day(2))
	up.Branch("side")
	side := up.Commit("side", day(3))
	up.Switch("main")
	g, repo := syncFixture(t, up, testOptions(t))

	head, err := g.Head(t.Context(), repo)
	if err != nil || head != second {
		t.Errorf("Head() = %q, %v, want %q", head, err, second)
	}

	missingBranch := repo
	missingBranch.DefaultBranch = "side" // only main was fetched
	if _, err := g.Head(t.Context(), missingBranch); err == nil || !strings.Contains(err.Error(), `branch "side" not found`) {
		t.Errorf("Head(unfetched branch) error = %v, want branch not found", err)
	}
	missingClone := repo
	missingClone.ClonePath = t.TempDir() + "/nope.git"
	if _, err := g.Head(t.Context(), missingClone); err == nil || !strings.Contains(err.Error(), "clone missing") {
		t.Errorf("Head(missing clone) error = %v, want clone missing", err)
	}

	tests := []struct {
		name             string
		ancestor, commit string
		want             bool
	}{
		{"older is ancestor", first, second, true},
		{"same commit", second, second, true},
		{"newer is not", second, first, false},
	}
	for _, tt := range tests {
		got, err := g.IsAncestor(t.Context(), repo, tt.ancestor, tt.commit)
		if err != nil || got != tt.want {
			t.Errorf("IsAncestor(%s) = %v, %v, want %v", tt.name, got, err, tt.want)
		}
	}
	// The side branch was never fetched, so its commit is unknown here: as
	// an ancestor it is simply not one; as the descendant it is an error.
	if ok, err := g.IsAncestor(t.Context(), repo, side, second); err != nil || ok {
		t.Errorf("IsAncestor(unknown ancestor) = %v, %v, want false, nil", ok, err)
	}
	if _, err := g.IsAncestor(t.Context(), repo, first, side); err == nil {
		t.Error("IsAncestor(unknown commit) = nil error, want an error")
	}
	if _, err := g.IsAncestor(t.Context(), repo, side, side); err == nil {
		t.Error("IsAncestor(both unknown) = nil error, want an error")
	}
	if _, err := g.IsAncestor(t.Context(), repo, "--all", second); err == nil {
		t.Error("IsAncestor(option) = nil error, want an error")
	}
}
