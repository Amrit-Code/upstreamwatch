package web

import (
	"context"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
)

// HealthReader supplies what GET /health reports.
type HealthReader interface {
	// LastRuns returns the most recent run of each job, ordered by job name.
	LastRuns(ctx context.Context) ([]model.Run, error)
}

// FindingsReader supplies the findings list page.
type FindingsReader interface {
	// OpenFindings returns the findings to show on GET /, highest score first.
	OpenFindings(ctx context.Context) ([]model.Finding, error)
}

// TimelineReader supplies the repo list and timeline pages.
type TimelineReader interface {
	// Repos returns every stored repo ordered by slug.
	Repos(ctx context.Context) ([]model.TargetRepo, error)
	// Timeline returns the repo and its intervals that pass f. An unknown
	// slug is an error wrapping model.ErrNotFound.
	Timeline(ctx context.Context, slug string, f query.TimelineFilter) (model.TargetRepo, []model.DepInterval, error)
}
