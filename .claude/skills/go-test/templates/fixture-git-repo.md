> Built in M1 as `internal/testutil/gitrepo` with a different API; read that package's doc comments. The code below is the original design and is kept for its reasoning, not to copy. `gitsrc.Options` has `ReposDir`, `HomeDir` and `AllowFileURLs`, not `DataDir`.

# Template: fixture git repositories

Use this when a test needs a real git history: the `gitsrc` adapter, or end-to-end timeline tests. Service tests for `timeline` use a fake `GitSource` instead.

The helper lives in `internal/testutil/gitfixture` so every package can use it. It shells out to the `git` binary. `git` is a runtime requirement of `upw`, so a missing binary fails the test rather than skipping it.

## What makes it deterministic

- Every `git` call gets an explicit environment and does not inherit `os.Environ()`. `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null` and a temp `HOME` keep the developer's config, aliases, hooks, signing and default branch out.
- Author and committer name, email and date are fixed per commit. The SHA depends only on content, metadata and parents, so the same fixture gives the same SHAs on every machine and git version that uses SHA-1 object names.
- `LC_ALL=C` and `TZ=UTC` keep git's own output stable.
- The environment goes through `exec.Cmd.Env`, never `t.Setenv`, so tests using the helper can run in parallel.

## Helper

```go
// Package gitfixture builds small git repositories for tests.
package gitfixture

// Repo is a non-bare repository in a temp directory.
type Repo struct {
	t   testing.TB
	Dir string
	env []string
}

// New creates an empty repository with branch main.
func New(t testing.TB) *Repo {
	t.Helper()
	home := t.TempDir()
	r := &Repo{
		t:   t,
		Dir: t.TempDir(),
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + home,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_TERMINAL_PROMPT=0",
			"LC_ALL=C",
			"TZ=UTC",
		},
	}
	r.git(time.Time{}, "init", "--initial-branch=main", ".")
	return r
}

// Write creates or replaces a file, creating parent directories.
func (r *Repo) Write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.Dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatalf("write %s: %v", path, err)
	}
}

// Remove deletes a file from the work tree.
func (r *Repo) Remove(path string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.Dir, filepath.FromSlash(path))); err != nil {
		r.t.Fatalf("remove %s: %v", path, err)
	}
}

// Commit stages everything and commits it at the given time. It returns the new SHA.
func (r *Repo) Commit(msg string, at time.Time) string {
	r.t.Helper()
	r.git(at, "add", "-A")
	r.git(at, "commit", "--allow-empty", "-m", msg)
	return r.git(at, "rev-parse", "HEAD")
}

// Branch creates a branch at HEAD and switches to it.
func (r *Repo) Branch(name string) { r.t.Helper(); r.git(time.Time{}, "switch", "-c", name) }

// Switch changes to an existing branch.
func (r *Repo) Switch(name string) { r.t.Helper(); r.git(time.Time{}, "switch", name) }

// Merge merges branch into the current branch with a merge commit, even when a
// fast-forward is possible, so first-parent walks have something to skip.
func (r *Repo) Merge(branch, msg string, at time.Time) string {
	r.t.Helper()
	r.git(at, "merge", "--no-ff", "-m", msg, branch)
	return r.git(at, "rev-parse", "HEAD")
}

// Bare clones the repository as a bare repository and returns its path.
// gitsrc reads bare clones only.
func (r *Repo) Bare() string {
	r.t.Helper()
	dst := filepath.Join(r.t.TempDir(), "fixture.git")
	r.git(time.Time{}, "clone", "--bare", "--quiet", r.Dir, dst)
	return dst
}

func (r *Repo) git(at time.Time, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append([]string(nil), r.env...)
	if !at.IsZero() {
		date := fmt.Sprintf("%d +0000", at.Unix()) // git's internal date format
		cmd.Env = append(cmd.Env,
			"GIT_AUTHOR_NAME=Fixture Author", "GIT_AUTHOR_EMAIL=author@example.com", "GIT_AUTHOR_DATE="+date,
			"GIT_COMMITTER_NAME=Fixture Committer", "GIT_COMMITTER_EMAIL=committer@example.com", "GIT_COMMITTER_DATE="+date,
		)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
```

## Using it: a first-parent case

```go
func day(n int) time.Time { return time.Date(2026, 1, n, 12, 0, 0, 0, time.UTC) }

func TestManifestCommits_FirstParentOnly(t *testing.T) {
	t.Parallel()
	r := gitfixture.New(t)
	r.Write("go.mod", "module example.com/app\n\ngo 1.27\n\nrequire github.com/example/lib v1.2.0\n")
	base := r.Commit("add lib", day(1))

	r.Branch("feature")
	r.Write("go.mod", "module example.com/app\n\ngo 1.27\n\nrequire github.com/example/lib v1.3.0\n")
	r.Commit("bump lib on a branch", day(2)) // not on the first-parent chain
	r.Switch("main")
	merge := r.Merge("feature", "merge feature", day(3))

	src := gitsrc.New(gitsrc.Options{DataDir: t.TempDir()}, slog.New(slog.DiscardHandler))
	repo := model.TargetRepo{Slug: "fixture", ClonePath: r.Bare(), DefaultBranch: "main"}

	got, err := src.ManifestCommits(t.Context(), repo, "", []string{"go.mod"})
	if err != nil {
		t.Fatalf("ManifestCommits() error = %v", err)
	}
	want := []string{base, merge}
	if !reflect.DeepEqual(shas(got), want) {
		t.Errorf("ManifestCommits() SHAs = %v, want %v", shas(got), want)
	}
}
```

Cases worth building fixtures for:

- a manifest added, bumped, then deleted;
- a manifest renamed (delete plus add);
- two manifests pinning different versions of one package;
- a bump on a branch that was reverted before merge (contributes nothing);
- a merge commit carrying a bump (dated at merge time);
- an incremental run after new commits (only the new range is read).

`Sync` (clone and fetch) goes through the https-only URL policy and is covered by the rules in `outbound-client`. Test the read methods against the bare clone the helper makes.
