package model

import (
	"errors"
	"time"
)

// TargetRepo is one repository whose dependency history is mined. The
// configured fields come from the config file; the stored fields from the
// repos table.
type TargetRepo struct {
	// ID is the repos row ID; zero before the repo is first stored.
	ID   int64
	Slug string
	URL  string
	// Branch is the configured branch; empty means the remote's default.
	Branch string
	// DefaultBranch is the branch actually indexed: Branch when set,
	// otherwise what the remote reported as HEAD at the last fetch.
	DefaultBranch string
	// ClonePath is the bare clone's directory.
	ClonePath string
	// LastIndexedSHA is the branch commit the timeline was last built to;
	// empty before the first timeline run.
	LastIndexedSHA string
	// LastFetchAt is the last fetch attempt, successful or not; zero before
	// the first. LastFetchError is that attempt's error text, empty on
	// success.
	LastFetchAt    time.Time
	LastFetchError string

	// Not stored. Token is the credential sent when fetching, zero for a
	// target with auth none. AllowPrivateHost exempts the target's host from
	// the private-address check.
	Token            Secret
	AllowPrivateHost bool
}

// SyncResult is what a clone or fetch of one target produced.
type SyncResult struct {
	// Branch is the branch fetched: the configured one, or the remote's
	// default when none is configured.
	Branch string
	// Head is the commit the local branch points at after the fetch.
	Head string
	// ClonePath is the bare clone's directory.
	ClonePath string
	// Cloned is true when this sync created the clone.
	Cloned bool
}

// FetchResult records one fetch attempt of a target in the repos table.
type FetchResult struct {
	RepoID int64
	At     time.Time
	// Branch and ClonePath are stored only when Error is empty.
	Branch    string
	ClonePath string
	// Error is the failure text, empty on success.
	Error string
}

// Commit is a commit on the first-parent chain of the indexed branch that
// touched at least one manifest path.
type Commit struct {
	SHA string
	// CommittedAt is the committer date in UTC: the time the commit landed on
	// the branch, which for a merge is the merge time.
	CommittedAt time.Time
	AuthorName  string
	AuthorEmail string
	Subject     string
	// Paths are the manifest paths the commit changed relative to its first
	// parent, including deleted ones.
	Paths []string
}

// BlobRef names one file at one commit.
type BlobRef struct {
	Commit string
	Path   string
}

// Blob is the content of one file at one commit.
type Blob struct {
	Ref     BlobRef
	Content []byte
	// Missing is true when the path does not exist at that commit, as after a
	// deletion. TooLarge is true when the file exceeds the size cap; Content
	// is then empty.
	Missing  bool
	TooLarge bool
}

// ManifestKind says whether a file declares requirements or pins resolved
// versions, as stored in manifests.kind.
type ManifestKind string

// The stored values of ManifestKind. A stored value is never renamed.
const (
	ManifestKindManifest ManifestKind = "manifest"
	ManifestKindLockfile ManifestKind = "lockfile"
)

// Directness says whether a dependency is declared by the project itself.
// The store keeps it in a nullable integer column: NULL, 1 or 0.
type Directness int8

// The values of Directness.
const (
	DirectUnknown Directness = iota
	DirectYes
	DirectNo
)

// Package identifies a package without a version.
type Package struct {
	// PURL is the version-less package URL, for example
	// "pkg:golang/github.com/gin-gonic/gin" or "pkg:golang/toolchain".
	PURL      string
	Ecosystem string
	Name      string
}

// Dependency is one package as one manifest file declares it at one commit.
type Dependency struct {
	Package Package
	// Version is the resolved version when the file gives one (a lockfile,
	// a go.mod require line, an exact pin, an image tag); empty otherwise.
	Version string
	// Requirement is the constraint or reference as written; for a Docker
	// image pinned by digest it holds the digest.
	Requirement string
	Direct      Directness
	// Scope is runtime, development, test, build or optional; empty when the
	// file does not say.
	Scope string
}

// ParsedManifest is one manifest file's content at one commit.
type ParsedManifest struct {
	Ecosystem    string
	Kind         ManifestKind
	Dependencies []Dependency
}

// ErrUnsupportedFile is returned, wrapped, by an extractor asked to parse a
// path it does not handle.
var ErrUnsupportedFile = errors.New("unsupported manifest file")

// Change is the kind of a dependency event, as stored in dep_events.change.
type Change string

// The stored values of Change. A stored value is never renamed.
const (
	ChangeAdded   Change = "added"
	ChangeChanged Change = "changed"
	ChangeRemoved Change = "removed"
)

// DepEvent is one change to one package in one manifest at one commit.
type DepEvent struct {
	CommitSHA    string
	CommittedAt  time.Time
	ManifestPath string
	Package      Package
	Change       Change
	// Version, Requirement, Direct and Scope describe the package after the
	// change; for a removal they repeat the values it had before.
	Version     string
	Requirement string
	Direct      Directness
	Scope       string
	// PrevVersion is the version before a change or removal, empty for an
	// addition.
	PrevVersion string
}

// DepInterval is a span of commits on the indexed branch during which one
// manifest pinned one package at one version.
type DepInterval struct {
	// ID is the dep_versions row ID; zero before it is stored.
	ID           int64
	ManifestPath string
	Package      Package
	Version      string
	Requirement  string
	Direct       Directness
	FromSHA      string
	From         time.Time
	// ToSHA and To are empty and zero while the interval is still open.
	ToSHA string
	To    time.Time
}

// IsOpen reports whether the interval is still in force at the indexed head.
func (iv DepInterval) IsOpen() bool { return iv.ToSHA == "" }

// Contains reports whether the interval covers t: From <= t, and t < To when
// the interval is closed.
func (iv DepInterval) Contains(t time.Time) bool {
	if t.Before(iv.From) {
		return false
	}
	return iv.IsOpen() || t.Before(iv.To)
}

// TimelineUpdate is everything one timeline run writes for one repo. The
// store applies it in one transaction.
type TimelineUpdate struct {
	RepoID int64
	// Reset discards the repo's stored commits, events and intervals before
	// writing, for a branch whose history was rewritten.
	Reset bool
	// Commits are the newly walked commits, oldest first. The store numbers
	// them after the repo's last stored commit.
	Commits []Commit
	// Manifests maps each manifest path seen in Commits to its parse
	// identity.
	Manifests map[string]ManifestInfo
	// Events are the new events, in commit order.
	Events []DepEvent
	// Intervals is the repo's complete interval set after this run.
	Intervals []DepInterval
	// IndexedSHA becomes the repo's last indexed commit.
	IndexedSHA string
}

// ManifestInfo is the ecosystem and kind of one manifest path.
type ManifestInfo struct {
	Ecosystem string
	Kind      ManifestKind
}
