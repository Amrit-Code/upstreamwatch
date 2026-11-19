package ingest

import (
	"context"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// GitSource clones and fetches target repositories.
type GitSource interface {
	// Sync checks repo's URL and host against the policy, then clones the
	// repository as a bare clone the first time and fetches its branch after
	// that. repo.Branch empty means the remote's default branch. repo.Token,
	// when set, is sent as the credential and never written to disk.
	Sync(ctx context.Context, repo model.TargetRepo) (model.SyncResult, error)
}

// RepoStore keeps one row per target repository in the repos table.
type RepoStore interface {
	// UpsertRepo inserts a row for slug the first time and updates its URL
	// after that. It returns the stored repo with its ID set.
	UpsertRepo(ctx context.Context, slug, url string) (model.TargetRepo, error)
	// RecordFetch stores one fetch attempt: the time and error text always;
	// the branch and clone path only when the attempt succeeded. An unknown
	// repo ID is an error wrapping model.ErrNotFound.
	RecordFetch(ctx context.Context, r model.FetchResult) error
}
