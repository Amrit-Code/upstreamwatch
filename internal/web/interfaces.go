package web

import (
	"context"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
)

// HealthReader supplies what GET /health reports.
type HealthReader interface {
	// LastRuns returns the most recent run of each job, ordered by job name.
	LastRuns(ctx context.Context) ([]model.Run, error)
}

// FindingsReader supplies the findings list and the finding page.
type FindingsReader interface {
	// Findings returns the detail of every unresolved finding that passes
	// f, highest score first. A zero f.Now means the current time.
	Findings(ctx context.Context, f model.FindingFilter) ([]model.FindingDetail, error)
	// FindingDetail returns one finding with everything shown with it. An
	// unknown ID is an error wrapping model.ErrNotFound.
	FindingDetail(ctx context.Context, id int64) (model.FindingDetail, error)
}

// FindingActions changes the operator state of a finding. Each method returns
// the new state; an unknown ID is an error wrapping model.ErrNotFound and a
// change the finding does not allow, a snooze length other than 1, 7 or 30
// days included, is an error wrapping model.ErrInvalidTransition.
type FindingActions interface {
	// Ack marks the finding acknowledged with note.
	Ack(ctx context.Context, id int64, note, by string) (model.FindingState, error)
	// Snooze hides the finding for d with note.
	Snooze(ctx context.Context, id int64, d time.Duration, note, by string) (model.FindingState, error)
	// Dismiss marks the finding dismissed with note.
	Dismiss(ctx context.Context, id int64, note, by string) (model.FindingState, error)
	// Reopen returns the finding to open.
	Reopen(ctx context.Context, id int64, by string) (model.FindingState, error)
}

// TimelineReader supplies the repo list and timeline pages.
type TimelineReader interface {
	// Repos returns every stored repo ordered by slug.
	Repos(ctx context.Context) ([]model.TargetRepo, error)
	// Timeline returns the repo and its intervals that pass f. An unknown
	// slug is an error wrapping model.ErrNotFound.
	Timeline(ctx context.Context, slug string, f query.TimelineFilter) (model.TargetRepo, []model.DepInterval, error)
}
