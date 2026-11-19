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

// FindingStore reads findings with their state and everything shown with
// them.
type FindingStore interface {
	// ListFindings returns the unresolved findings that pass f, highest
	// score first, then lowest ID. f.Now must be set.
	ListFindings(ctx context.Context, f model.FindingFilter) ([]model.Finding, error)
	// GetFindingDetail returns the finding with this ID and everything shown
	// with it, or an error wrapping model.ErrNotFound.
	GetFindingDetail(ctx context.Context, id int64) (model.FindingDetail, error)
}
