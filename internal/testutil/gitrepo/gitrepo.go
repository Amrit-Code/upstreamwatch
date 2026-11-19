// Package gitrepo builds small non-bare git repositories in a temp directory
// for tests. Author and committer identities are fixed and every commit takes
// an explicit date, so SHAs and timestamps come out the same on every machine.
// Any git failure fails the test.
package gitrepo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixed identities every commit is made with.
const (
	AuthorName     = "Fixture Author"
	AuthorEmail    = "author@example.com"
	CommitterName  = "Fixture Committer"
	CommitterEmail = "committer@example.com"
)

// Repo is a non-bare repository in a temp directory.
type Repo struct {
	t   testing.TB
	dir string
	env []string
}

// New creates an empty repository whose first branch is initialBranch.
func New(t testing.TB, initialBranch string) *Repo {
	t.Helper()
	r := &Repo{t: t, dir: t.TempDir(), env: environ(t.TempDir())}
	r.git(time.Time{}, "init", "--quiet", "--initial-branch="+initialBranch, ".")
	return r
}

// Path returns the repository's work tree directory.
func (r *Repo) Path() string { return r.dir }

// URL returns the repository's file:// URL.
func (r *Repo) URL() string { return "file://" + filepath.ToSlash(r.dir) }

// Write creates or replaces a file in the work tree, creating parent
// directories.
func (r *Repo) Write(path, content string) {
	r.t.Helper()
	r.WriteBytes(path, []byte(content))
}

// WriteBytes creates or replaces a file in the work tree with raw content.
func (r *Repo) WriteBytes(path string, content []byte) {
	r.t.Helper()
	full := filepath.Join(r.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("gitrepo: mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		r.t.Fatalf("gitrepo: write %s: %v", path, err)
	}
}

// Delete removes a file from the work tree.
func (r *Repo) Delete(path string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, filepath.FromSlash(path))); err != nil {
		r.t.Fatalf("gitrepo: delete %s: %v", path, err)
	}
}

// Commit stages every change in the work tree and commits it with author and
// committer date at. It returns the new commit's SHA.
func (r *Repo) Commit(msg string, at time.Time) string {
	r.t.Helper()
	r.git(at, "add", "--all")
	r.git(at, "commit", "--quiet", "--allow-empty", "--no-verify", "-m", msg)
	return r.Head()
}

// Branch creates a branch at the current commit and switches to it.
func (r *Repo) Branch(name string) {
	r.t.Helper()
	r.git(time.Time{}, "switch", "--quiet", "-c", name)
}

// Switch changes to an existing branch.
func (r *Repo) Switch(name string) {
	r.t.Helper()
	r.git(time.Time{}, "switch", "--quiet", name)
}

// Merge merges branch into the current branch with a merge commit, even when
// a fast-forward is possible, dated at. It returns the merge commit's SHA.
func (r *Repo) Merge(branch, msg string, at time.Time) string {
	r.t.Helper()
	r.git(at, "merge", "--quiet", "--no-ff", "--no-verify", "-m", msg, branch)
	return r.Head()
}

// Head returns the SHA of the current commit.
func (r *Repo) Head() string {
	r.t.Helper()
	return r.git(time.Time{}, "rev-parse", "HEAD")
}

// Git runs git with args in the repository and returns its trimmed output.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	return r.git(time.Time{}, args...)
}

func (r *Repo) git(at time.Time, args ...string) string {
	r.t.Helper()
	env := r.env
	if !at.IsZero() {
		// "@<seconds>" is git's unambiguous raw form; a bare small number
		// would be read as a calendar date.
		date := fmt.Sprintf("@%d +0000", at.Unix())
		env = append(append([]string(nil), r.env...),
			"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	}
	return run(r.t, r.dir, env, args...)
}

// Git runs git with args in dir, with the same isolated environment as a
// Repo and a temporary HOME, and returns its trimmed output. Tests use it to
// inspect directories that are not fixture repositories, such as a bare
// clone.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	return run(t, dir, environ(t.TempDir()), args...)
}

// environ returns the environment fixture git processes run with: no user or
// system config, the given HOME, the fixed identities, C locale and UTC.
func environ(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
		"TZ=UTC",
		"GIT_AUTHOR_NAME=" + AuthorName,
		"GIT_AUTHOR_EMAIL=" + AuthorEmail,
		"GIT_COMMITTER_NAME=" + CommitterName,
		"GIT_COMMITTER_EMAIL=" + CommitterEmail,
	}
}

func run(t testing.TB, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gitrepo: git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
