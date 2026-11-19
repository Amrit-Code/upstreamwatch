package query

import (
	"context"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// RunStore reads the runs table.
type RunStore interface {
	// LatestRuns returns the most recent run of each job, ordered by job name.
	LatestRuns(ctx context.Context) ([]model.Run, error)
}

// TimelineStore reads target repos and their dependency intervals.
type TimelineStore interface {
	// GetRepo returns the repo stored under slug, or an error wrapping
	// model.ErrNotFound.
	GetRepo(ctx context.Context, slug string) (model.TargetRepo, error)
	// ListRepos returns every stored repo ordered by slug.
	ListRepos(ctx context.Context) ([]model.TargetRepo, error)
	// ListIntervals returns the repo's intervals with their row IDs, ordered
	// by package URL, then manifest path, then opening time, then ID.
	ListIntervals(ctx context.Context, repoID int64) ([]model.DepInterval, error)
}

// FindingStore reads findings with their state.
type FindingStore interface {
	// ListOpenFindings returns unresolved findings whose state is open or
	// acked, highest score first, then lowest ID.
	ListOpenFindings(ctx context.Context) ([]model.Finding, error)
}
