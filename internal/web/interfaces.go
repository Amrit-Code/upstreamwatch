package web

import (
	"context"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
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
