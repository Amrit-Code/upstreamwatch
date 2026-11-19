package gitsrc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Head returns the commit refs/heads/<repo.DefaultBranch> points at in
// repo.ClonePath.
func (g *CLI) Head(ctx context.Context, repo model.TargetRepo) (string, error) {
	clone, err := g.prepareClone(repo)
	if err != nil {
		return "", fmt.Errorf("head of %s: %w", repo.Slug, err)
	}
	if err := g.checkBranch(ctx, repo.DefaultBranch); err != nil {
		return "", fmt.Errorf("head of %s: %w", repo.Slug, err)
	}
	head, err := g.branchHead(ctx, clone, repo.DefaultBranch)
	if err != nil {
		return "", fmt.Errorf("head of %s: %w", repo.Slug, err)
	}
	return head, nil
}

// IsAncestor reports whether ancestor is reachable from commit in
// repo.ClonePath.
func (g *CLI) IsAncestor(ctx context.Context, repo model.TargetRepo, ancestor, commit string) (bool, error) {
	clone, err := g.prepareClone(repo)
	if err != nil {
		return false, fmt.Errorf("is-ancestor in %s: %w", repo.Slug, err)
	}
	if err := checkRevision(ancestor); err != nil {
		return false, fmt.Errorf("is-ancestor in %s: %w", repo.Slug, err)
	}
	if err := checkRevision(commit); err != nil {
		return false, fmt.Errorf("is-ancestor in %s: %w", repo.Slug, err)
	}
	err = g.run(ctx, invocation{
		gitDir: clone,
		args:   []string{"merge-base", "--is-ancestor", "--end-of-options", ancestor, commit},
	})
	switch {
	case err == nil:
		return true, nil
	case exitCode(err) == 1 && ctx.Err() == nil:
		return false, nil
	default:
		return false, fmt.Errorf("is-ancestor in %s: %w", repo.Slug, err)
	}
}

// commitMarker starts every commit record in ManifestCommits' log output.
// Fields and paths are NUL-terminated and cannot contain NUL. A record
// starts at a token equal to the marker that is followed by a commit SHA;
// a path matching a manifest pattern cannot look like that.
const commitMarker = "\x01upw-commit"

// logHeaderTokens is the number of NUL-terminated tokens logFormat prints
// (the marker and five fields), followed by one empty token that git's -z
// puts between a commit's header and its names.
const logHeaderTokens = 6

// logFormat prints the marker and the commit's fields, each NUL-terminated.
const logFormat = "%x01upw-commit%x00%H%x00%ct%x00%an%x00%ae%x00%s%x00"

// ManifestCommits returns the commits on the first-parent chain ending at to,
// after from (from the root commit when from is empty), that changed a path
// matching one of patterns relative to their first parent, oldest first.
// Each commit's Paths are the matching paths it added, changed or deleted,
// sorted.
func (g *CLI) ManifestCommits(ctx context.Context, repo model.TargetRepo, from, to string, patterns []string) ([]model.Commit, error) {
	if len(patterns) == 0 {
		return nil, fmt.Errorf("manifest commits of %s: no patterns", repo.Slug)
	}
	clone, err := g.prepareClone(repo)
	if err != nil {
		return nil, fmt.Errorf("manifest commits of %s: %w", repo.Slug, err)
	}
	if err := checkRevision(to); err != nil {
		return nil, fmt.Errorf("manifest commits of %s: %w", repo.Slug, err)
	}
	revs := []string{to}
	if from != "" {
		if err := checkRevision(from); err != nil {
			return nil, fmt.Errorf("manifest commits of %s: %w", repo.Slug, err)
		}
		revs = append(revs, "^"+from)
	}

	// --first-parent walks only the branch's own chain. --full-history and
	// --sparse turn off history simplification, so every commit on that
	// chain is printed and none is dropped or replaced because it is
	// TREESAME to some parent; commits with no matching path are filtered
	// below. --diff-merges=first-parent gives a merge the diff against its
	// first parent, which is what the branch saw change. --no-renames lists
	// a rename as the deleted and the added path.
	args := []string{
		"log", "--first-parent", "--full-history", "--sparse",
		"--diff-merges=first-parent", "--no-renames", "--name-only",
		"--reverse", "-z", "--no-color", "--format=" + logFormat,
		"--end-of-options",
	}
	args = append(args, revs...)
	args = append(args, "--")
	for _, p := range patterns {
		if p == "" {
			// ":(glob)" alone would match every path.
			return nil, fmt.Errorf("manifest commits of %s: empty pattern", repo.Slug)
		}
		args = append(args, ":(glob)"+p)
	}

	var stdout bytes.Buffer
	if err := g.run(ctx, invocation{gitDir: clone, args: args, stdout: &stdout}); err != nil {
		return nil, fmt.Errorf("manifest commits of %s: %w", repo.Slug, err)
	}
	commits, err := parseLog(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("manifest commits of %s: %w", repo.Slug, err)
	}
	return commits, nil
}

// parseLog reads the output of the log command in ManifestCommits and keeps
// the commits that list at least one path. Each record is the header tokens,
// an empty separator token, then the names, the first one prefixed with a
// newline.
func parseLog(out []byte) ([]model.Commit, error) {
	tokens := strings.Split(string(out), "\x00")
	var commits []model.Commit
	for i := 0; i < len(tokens); {
		if tokens[i] == "" {
			i++ // the empty token after the last NUL
			continue
		}
		if !recordStart(tokens, i) {
			return nil, fmt.Errorf("parse git log: unexpected token at field %d", i)
		}
		if i+logHeaderTokens >= len(tokens) || tokens[i+logHeaderTokens] != "" {
			return nil, errors.New("parse git log: truncated commit record")
		}
		secs, err := strconv.ParseInt(tokens[i+2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse git log: committer date of %s: %w", tokens[i+1], err)
		}
		c := model.Commit{
			SHA:         tokens[i+1],
			CommittedAt: time.Unix(secs, 0).UTC(),
			AuthorName:  tokens[i+3],
			AuthorEmail: tokens[i+4],
			Subject:     tokens[i+5],
		}
		i += logHeaderTokens + 1
		for first := true; i < len(tokens) && !recordStart(tokens, i); i++ {
			p := tokens[i]
			if first {
				p = strings.TrimPrefix(p, "\n")
				first = false
			}
			if p != "" {
				c.Paths = append(c.Paths, p)
			}
		}
		if len(c.Paths) == 0 {
			continue
		}
		slices.Sort(c.Paths)
		c.Paths = slices.Compact(c.Paths)
		commits = append(commits, c)
	}
	return commits, nil
}

// recordStart reports whether tokens[i] starts a commit record: the marker
// followed by a commit SHA.
func recordStart(tokens []string, i int) bool {
	return tokens[i] == commitMarker && i+1 < len(tokens) && isObjectName(tokens[i+1])
}

// isObjectName reports whether s is a full SHA-1 or SHA-256 object name in
// lowercase hex.
func isObjectName(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range []byte(s) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// checkRevision refuses an empty revision or one that starts with "-".
func checkRevision(rev string) error {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return fmt.Errorf("revision %q not allowed", rev)
	}
	return nil
}

// prepareClone checks that repo.ClonePath is an existing directory, creates
// HomeDir, and returns the clone path made absolute, which is the form git
// must be given because it runs in HomeDir.
func (g *CLI) prepareClone(repo model.TargetRepo) (string, error) {
	if repo.ClonePath == "" {
		return "", errors.New("no clone path")
	}
	clone, err := absPath(repo.ClonePath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(clone)
	if err != nil {
		return "", fmt.Errorf("clone missing: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("clone path %s is not a directory", clone)
	}
	if err := g.prepareHome(); err != nil {
		return "", err
	}
	return clone, nil
}
