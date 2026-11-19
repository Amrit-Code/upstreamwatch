package pipeline

import (
	"context"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Job is one pipeline stage. The Runner is the only caller of Run.
type Job interface {
	// Name is the string used in the config schedules, in "upw run <job>" and
	// in the runs table.
	Name() string
	// Run does the job's work for scope and reports counters.
	Run(ctx context.Context, scope model.Scope) (model.Stats, error)
}

// RunStore records job runs in the runs table.
type RunStore interface {
	// StartRun inserts a run with status model.RunRunning and returns it with
	// its ID set.
	StartRun(ctx context.Context, job string, startedAt time.Time) (model.Run, error)
	// FinishRun stores FinishedAt, Status, Stats and Error for run.ID. An
	// unknown ID is an error wrapping model.ErrNotFound.
	FinishRun(ctx context.Context, run model.Run) error
}

// FindingStore raises and clears findings.
type FindingStore interface {
	// UpsertFinding inserts f, keyed by f.Fingerprint, in state open with
	// first-seen and last-changed times taken from f.LastChangedAt. If the
	// fingerprint exists it updates score and rank reason, clears the
	// resolved time, and leaves the operator state alone; the last-changed
	// time moves to f.LastChangedAt only when the score or rank reason
	// differs or the finding was resolved. It returns the stored finding.
	UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error)
	// ResolveFinding marks the unresolved finding with this fingerprint as
	// resolved at the given time. It is not an error if there is none.
	ResolveFinding(ctx context.Context, fingerprint string, at time.Time) error
}
