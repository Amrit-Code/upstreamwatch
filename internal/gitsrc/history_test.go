package gitsrc_test

import (
	"reflect"
	"strings"
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
	// The side branch was never fetched, so its commit is unknown here.
	if _, err := g.IsAncestor(t.Context(), repo, side, second); err == nil {
		t.Error("IsAncestor(unknown commit) = nil error, want an error")
	}
	if _, err := g.IsAncestor(t.Context(), repo, "--all", second); err == nil {
		t.Error("IsAncestor(option) = nil error, want an error")
	}
}
