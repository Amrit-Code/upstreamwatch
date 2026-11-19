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

// FindingStore reads findings with their state.
type FindingStore interface {
	// ListOpenFindings returns unresolved findings whose state is open or
	// acked, highest score first, then lowest ID.
	ListOpenFindings(ctx context.Context) ([]model.Finding, error)
}
