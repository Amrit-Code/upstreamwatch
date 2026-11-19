package vuln

import (
	"context"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// AdvisorySource is an advisory database (OSV). Package URLs in and out are
// upstreamwatch's own; the adapter maps them to the database's ecosystems and
// names.
type AdvisorySource interface {
	// QueryBatch returns, for each queried pair, the advisories affecting
	// it, as refs with their modified time; the same id may appear for
	// several pairs and is returned once. Pairs whose package type has no
	// database ecosystem (model.OSVEcosystem false) are skipped. The adapter
	// splits large inputs into batches itself.
	QueryBatch(ctx context.Context, pvs []model.PackageVersion) ([]model.AdvisoryRef, error)
	// Fetch returns the full record for id. An unknown id is an error
	// wrapping model.ErrNotFound.
	Fetch(ctx context.Context, id string) (model.Advisory, error)
	// ModifiedSince returns the refs of records in one database ecosystem
	// (an OSV ecosystem name such as "Go") modified strictly after since,
	// newest first.
	ModifiedSince(ctx context.Context, ecosystem string, since time.Time) ([]model.AdvisoryRef, error)
}

// ExploitSignals is one exploit-signal feed: CISA KEV or EPSS.
type ExploitSignals interface {
	// Refresh downloads the feed and returns every signal in it.
	Refresh(ctx context.Context) ([]model.Signal, error)
}

// AdvisoryStore holds advisories, the pairs already queried, and the
// advisory matches.
type AdvisoryStore interface {
	// ListAllIntervals returns every stored dependency interval of every
	// repo (open and closed) with repo and package IDs, ordered by repo
	// slug, then package URL, then manifest path, then opening time.
	ListAllIntervals(ctx context.Context) ([]model.RepoInterval, error)
	// QueriedVersions returns every pair already sent to QueryBatch.
	QueriedVersions(ctx context.Context) ([]model.PackageVersion, error)
	// MarkQueried records pvs as queried at the given time. Pairs already
	// recorded keep their row and get the new time.
	MarkQueried(ctx context.Context, pvs []model.PackageVersion, at time.Time) error
	// AdvisoryModified returns the stored modified time of each id that is
	// stored; ids not stored are absent from the map.
	AdvisoryModified(ctx context.Context, ids []string) (map[string]time.Time, error)
	// SaveAdvisory upserts a's advisories row (all columns from a except the
	// exploit-signal columns, which an update leaves alone) and replaces
	// its advisory_affected rows with a.Affected, inserting any package that
	// is not stored yet (by package URL). One transaction.
	SaveAdvisory(ctx context.Context, a model.Advisory) error
	// ListAdvisories returns every stored advisory with its Affected
	// entries and exploit signals, Raw left nil, ordered by id.
	ListAdvisories(ctx context.Context) ([]model.Advisory, error)
	// ApplySignals stores signals on every advisory whose id or one of whose
	// aliases equals the signal's CVE. A KEV signal sets kev = 1 and
	// kev_added_ts; an EPSS signal sets epss, epss_percentile and epss_ts =
	// at. Signals for CVEs no stored advisory names are ignored. It returns
	// the number of advisories updated. One transaction.
	ApplySignals(ctx context.Context, signals []model.Signal, at time.Time) (int, error)
	// ReplaceMatches makes matches the complete set of advisory matches for
	// repoID: rows whose (dep_version_id, advisory_id) is in matches are
	// updated in place (keeping their ID), missing ones inserted, and the
	// repo's other rows deleted. One transaction.
	ReplaceMatches(ctx context.Context, repoID int64, matches []model.AdvisoryMatch) error
}

// KVStore holds small named values: the feed cursors.
type KVStore interface {
	// GetKV returns the value stored under key, or an error wrapping
	// model.ErrNotFound.
	GetKV(ctx context.Context, key string) (string, error)
	// SetKV stores value under key, replacing any previous value.
	SetKV(ctx context.Context, key, value string) error
}

// FindingStore raises, refreshes and resolves advisory findings.
type FindingStore interface {
	// UpsertFinding inserts or refreshes f, keyed by f.Fingerprint. See
	// store.DB.UpsertFinding for which fields an update changes and when
	// the last-changed time moves.
	UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error)
	// ResolveFinding marks the unresolved finding with this fingerprint as
	// resolved at the given time. It is not an error if there is none.
	ResolveFinding(ctx context.Context, fingerprint string, at time.Time) error
	// ListUnresolvedFindings returns every unresolved finding of kind, in
	// any operator state, ordered by ID.
	ListUnresolvedFindings(ctx context.Context, kind model.FindingKind) ([]model.Finding, error)
}
