package gitsrc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// sizeCheckInterval is how often a running fetch's clone directory is
// measured against the size cap.
const sizeCheckInterval = time.Second

// slugPattern is what a slug must look like before it becomes a path, so
// "../" can never appear.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Sync checks repo's URL and host against the policy, then creates the bare
// clone and fetches the branch the first time, and fetches the branch after
// that. repo.Branch empty means the remote's default branch. No remote is
// added to the clone's config, so neither the URL nor the credential is
// stored on disk.
func (g *CLI) Sync(ctx context.Context, repo model.TargetRepo) (model.SyncResult, error) {
	if !slugPattern.MatchString(repo.Slug) {
		return model.SyncResult{}, fmt.Errorf("sync: slug %q must be lowercase letters, digits and hyphens", repo.Slug)
	}
	_, reposDir, err := g.dirs()
	if err != nil {
		return model.SyncResult{}, fmt.Errorf("sync %s: %w", repo.Slug, err)
	}
	if reposDir == "" {
		return model.SyncResult{}, fmt.Errorf("sync %s: gitsrc ReposDir is not set", repo.Slug)
	}
	// The URL check runs before anything touches the disk or starts git.
	if _, err := g.checkRemote(ctx, repo); err != nil {
		return model.SyncResult{}, fmt.Errorf("sync %s: %w", repo.Slug, err)
	}
	if err := g.prepareHome(); err != nil {
		return model.SyncResult{}, fmt.Errorf("sync %s: %w", repo.Slug, err)
	}

	branch := repo.Branch
	if branch == "" {
		b, err := g.remoteDefaultBranch(ctx, repo)
		if err != nil {
			return model.SyncResult{}, fmt.Errorf("sync %s: %w", repo.Slug, err)
		}
		branch = b
	}
	if err := g.checkBranch(ctx, branch); err != nil {
		return model.SyncResult{}, fmt.Errorf("sync %s: %w", repo.Slug, err)
	}

	if err := os.MkdirAll(reposDir, 0o700); err != nil {
		return model.SyncResult{}, fmt.Errorf("sync %s: create repos directory: %w", repo.Slug, err)
	}
	// One absolute path serves git, the size watch, cleanup and the result.
	dir := filepath.Join(reposDir, repo.Slug+".git")
	info, err := os.Stat(dir)
	var cloned bool
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := g.firstSync(ctx, repo, dir, branch); err != nil {
			return model.SyncResult{}, fmt.Errorf("sync %s: %w", repo.Slug, err)
		}
		cloned = true
	case err != nil:
		return model.SyncResult{}, fmt.Errorf("sync %s: %w", repo.Slug, err)
	case !info.IsDir():
		return model.SyncResult{}, fmt.Errorf("sync %s: clone path %s is not a directory", repo.Slug, dir)
	default:
		fctx, cancel := context.WithTimeout(ctx, g.opts.FetchTimeout)
		err := g.fetch(fctx, repo, dir, branch)
		cancel()
		if err != nil {
			return model.SyncResult{}, fmt.Errorf("sync %s: %w", repo.Slug, err)
		}
	}

	head, err := g.branchHead(ctx, dir, branch)
	if err != nil {
		if cloned {
			// A first sync that fails at any step leaves no clone behind.
			if rmErr := os.RemoveAll(dir); rmErr != nil {
				err = errors.Join(err, fmt.Errorf("remove failed clone: %w", rmErr))
			}
		}
		return model.SyncResult{}, fmt.Errorf("sync %s: %w", repo.Slug, err)
	}
	g.logger.Info("git sync finished", "repo", repo.Slug, "branch", branch, "head", head, "cloned", cloned)
	return model.SyncResult{Branch: branch, Head: head, ClonePath: dir, Cloned: cloned}, nil
}

// firstSync creates the bare clone at dir and fetches branch into it under
// the clone timeout. On any failure it removes dir, so the next sync starts
// clean.
func (g *CLI) firstSync(ctx context.Context, repo model.TargetRepo, dir, branch string) (err error) {
	ctx, cancel := context.WithTimeout(ctx, g.opts.CloneTimeout)
	defer cancel()

	if err := os.Mkdir(dir, 0o700); err != nil {
		return fmt.Errorf("create clone directory: %w", err)
	}
	defer func() {
		if err != nil {
			if rmErr := os.RemoveAll(dir); rmErr != nil {
				err = errors.Join(err, fmt.Errorf("remove failed clone: %w", rmErr))
			}
		}
	}()
	if err := g.run(ctx, invocation{args: []string{"init", "--bare", "--quiet", "--", dir}}); err != nil {
		return err
	}
	return g.fetch(ctx, repo, dir, branch)
}

// stagingRef is where a fetch lands before it is accepted, so a fetch that
// fails or passes the size cap never moves the branch.
func stagingRef(branch string) string { return "refs/upw/staging/" + branch }

// cleanupTimeout bounds deleting the staging ref after a failed fetch, which
// runs even when the fetch's own context has expired.
const cleanupTimeout = 30 * time.Second

// fetch fetches branch from repo's URL into the clone at dir, rechecking the
// URL and host first. The fetch writes the staging ref; refs/heads/<branch>
// moves to it only after the fetch succeeded and the clone is within the
// size cap. On failure the staging ref is deleted and the branch is left as
// it was; objects the fetch wrote stay until git collects them.
func (g *CLI) fetch(ctx context.Context, repo model.TargetRepo, dir, branch string) error {
	if err := g.fetchStaging(ctx, repo, dir, branch); err != nil {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if delErr := g.deleteRef(cctx, dir, stagingRef(branch)); delErr != nil {
			err = errors.Join(err, fmt.Errorf("delete staging ref: %w", delErr))
		}
		return err
	}
	staged, found, err := g.refCommit(ctx, dir, stagingRef(branch))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("fetch left no staging ref for branch %q", branch)
	}
	if err := g.deleteRef(ctx, dir, stagingRef(branch)); err != nil {
		return fmt.Errorf("delete staging ref: %w", err)
	}
	if err := g.run(ctx, invocation{
		gitDir: dir,
		args:   []string{"update-ref", "--end-of-options", "refs/heads/" + branch, staged},
	}); err != nil {
		return fmt.Errorf("move branch %q: %w", branch, err)
	}
	return nil
}

// deleteRef deletes ref in the clone at dir; a ref that does not exist is
// not an error.
func (g *CLI) deleteRef(ctx context.Context, dir, ref string) error {
	return g.run(ctx, invocation{gitDir: dir, args: []string{"update-ref", "-d", "--end-of-options", ref}})
}

// fetchStaging runs the fetch into the staging ref. While git runs, the
// clone directory is measured every sizeCheckInterval and git is stopped when
// it passes the size cap; the directory is measured once more after git
// exits.
func (g *CLI) fetchStaging(ctx context.Context, repo model.TargetRepo, dir, branch string) error {
	rem, err := g.checkRemote(ctx, repo)
	if err != nil {
		return err
	}

	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var exceeded atomic.Bool
	var wg sync.WaitGroup
	wg.Go(func() {
		tick := time.NewTicker(sizeCheckInterval)
		defer tick.Stop()
		for {
			select {
			case <-fctx.Done():
				return
			case <-tick.C:
				// A failed measurement is retried on the next tick and
				// repeated after git exits.
				if size, err := dirSize(dir); err == nil && size > g.opts.MaxCloneBytes {
					exceeded.Store(true)
					cancel()
					return
				}
			}
		}
	})

	refspec := "+refs/heads/" + branch + ":" + stagingRef(branch)
	runErr := g.run(fctx, invocation{
		gitDir:  dir,
		config:  rem.config,
		env:     rem.env,
		secrets: rem.secrets,
		args:    []string{"fetch", "--no-tags", "--end-of-options", rem.url, refspec},
	})
	cancel()
	wg.Wait()

	if exceeded.Load() {
		return g.capError()
	}
	if runErr != nil {
		return runErr
	}
	size, err := dirSize(dir)
	if err != nil {
		return fmt.Errorf("measure clone: %w", err)
	}
	if size > g.opts.MaxCloneBytes {
		return g.capError()
	}
	return nil
}

// capError reports a clone that grew past MaxCloneBytes.
func (g *CLI) capError() error {
	mib := strconv.FormatFloat(float64(g.opts.MaxCloneBytes)/(1<<20), 'f', -1, 64)
	return fmt.Errorf("clone exceeded the %s MiB size cap", mib)
}

// remoteDefaultBranch asks the remote for the branch its HEAD points at.
func (g *CLI) remoteDefaultBranch(ctx context.Context, repo model.TargetRepo) (string, error) {
	rem, err := g.checkRemote(ctx, repo)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, g.opts.FetchTimeout)
	defer cancel()
	out, err := g.output(ctx, invocation{
		config:  rem.config,
		env:     rem.env,
		secrets: rem.secrets,
		args:    []string{"ls-remote", "--symref", "--", rem.url, "HEAD"},
	})
	if err != nil {
		return "", err
	}
	branch, ok := parseSymref(out)
	if !ok {
		return "", fmt.Errorf("find default branch: %s reports no HEAD branch", rem.host)
	}
	return branch, nil
}

// parseSymref finds "ref: refs/heads/<branch>\tHEAD" in ls-remote --symref
// output.
func parseSymref(out string) (string, bool) {
	for line := range strings.Lines(out) {
		ref, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), "ref: refs/heads/")
		if !ok {
			continue
		}
		if branch, ok := strings.CutSuffix(ref, "\tHEAD"); ok && branch != "" {
			return branch, true
		}
	}
	return "", false
}

// checkBranch refuses a branch name that starts with "-" or that git
// check-ref-format --branch rejects or rewrites.
func (g *CLI) checkBranch(ctx context.Context, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return fmt.Errorf("branch %q not allowed", branch)
	}
	out, err := g.output(ctx, invocation{args: []string{"check-ref-format", "--branch", branch}})
	if err != nil {
		// With --branch, git dies (exit 128) on an invalid name; any exit
		// status is a rejection, anything else an operational failure.
		if exitCode(err) > 0 {
			return fmt.Errorf("branch %q not allowed: not a valid branch name", branch)
		}
		return fmt.Errorf("check branch %q: %w", branch, err)
	}
	if strings.TrimSpace(out) != branch {
		return fmt.Errorf("branch %q not allowed: not a plain branch name", branch)
	}
	return nil
}

// branchHead returns the commit refs/heads/<branch> points at in the clone at
// dir.
func (g *CLI) branchHead(ctx context.Context, dir, branch string) (string, error) {
	sha, found, err := g.refCommit(ctx, dir, "refs/heads/"+branch)
	if err != nil {
		return "", fmt.Errorf("resolve branch %q: %w", branch, err)
	}
	if !found {
		return "", fmt.Errorf("branch %q not found in %s", branch, dir)
	}
	return sha, nil
}

// refCommit returns the commit ref points at in the clone at dir; found is
// false when the ref does not exist.
func (g *CLI) refCommit(ctx context.Context, dir, ref string) (sha string, found bool, err error) {
	out, err := g.output(ctx, invocation{
		gitDir: dir,
		args:   []string{"rev-parse", "--verify", "--quiet", "--end-of-options", ref + "^{commit}"},
	})
	if err != nil {
		if exitCode(err) == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

// prepareHome creates HomeDir, the empty HOME every git process gets.
func (g *CLI) prepareHome() error {
	home, _, err := g.dirs()
	if err != nil {
		return err
	}
	if home == "" {
		return errors.New("gitsrc HomeDir is not set")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create git home: %w", err)
	}
	return nil
}

// dirSize returns the total size of the regular files under dir. Files that
// disappear while it walks, as git's temporary files do, are skipped.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}
