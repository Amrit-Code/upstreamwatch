package timeline

import (
	"context"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// GitSource reads history from a target's bare clone. None of its methods
// touches the network.
type GitSource interface {
	// Head returns the commit repo.DefaultBranch points at in the clone.
	Head(ctx context.Context, repo model.TargetRepo) (string, error)
	// OnFirstParentChain reports whether commit is on the first-parent chain
	// ending at head (head itself counts). A commit that does not exist in
	// the clone is not on the chain.
	OnFirstParentChain(ctx context.Context, repo model.TargetRepo, commit, head string) (bool, error)
	// ManifestCommits returns the commits on the first-parent chain ending
	// at to, after from (from empty means from the root commit), that
	// changed a path matching one of patterns relative to their first
	// parent, oldest first. Patterns use git's glob pathspec syntax, so
	// "**/go.mod" also matches a go.mod at the root. Each commit's Paths are
	// the matching paths it changed, deletions included.
	ManifestCommits(ctx context.Context, repo model.TargetRepo, from, to string, patterns []string) ([]model.Commit, error)
	// ReadBlobs returns one Blob per ref, in the order given, through one
	// long-lived git cat-file process.
	ReadBlobs(ctx context.Context, repo model.TargetRepo, refs []model.BlobRef) ([]model.Blob, error)
}

// Extractor turns one manifest file into dependencies.
type Extractor interface {
	// Patterns returns the paths this extractor reads, in git's glob
	// pathspec syntax.
	Patterns() []string
	// Parse parses one file. A path the extractor does not handle is an
	// error wrapping model.ErrUnsupportedFile.
	Parse(path string, content []byte) (model.ParsedManifest, error)
}

// RepoStore reads target repositories.
type RepoStore interface {
	// GetRepo returns the repo stored under slug, or an error wrapping
	// model.ErrNotFound.
	GetRepo(ctx context.Context, slug string) (model.TargetRepo, error)
}

// TimelineStore reads and writes a repo's commits, manifests, packages,
// dependency events and intervals.
type TimelineStore interface {
	// ListEvents returns every stored event of the repo, ordered by commit
	// position, then manifest path, then package URL.
	ListEvents(ctx context.Context, repoID int64) ([]model.DepEvent, error)
	// ApplyTimeline writes u in one transaction: with u.Reset it first
	// deletes the repo's commits, events and intervals; it then appends
	// u.Commits after the repo's last stored position, upserts manifests
	// and packages, inserts u.Events, replaces the repo's intervals with
	// u.Intervals while keeping the row ID of every interval whose manifest,
	// package and opening commit are unchanged, and sets the repo's last
	// indexed commit to u.IndexedSHA and its timeline settings fingerprint to
	// u.Settings.
	ApplyTimeline(ctx context.Context, u model.TimelineUpdate) error
}
