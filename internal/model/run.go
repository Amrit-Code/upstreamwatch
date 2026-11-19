package model

import "time"

// RunStatus is the outcome of a job run, as stored in runs.status.
type RunStatus string

// The stored values of RunStatus. A stored value is never renamed.
const (
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
)

// Run is one execution of a job, as recorded in the runs table.
type Run struct {
	ID  int64
	Job string
	// StartedAt and FinishedAt are UTC. FinishedAt is zero while the run is
	// still running.
	StartedAt  time.Time
	FinishedAt time.Time
	Status     RunStatus
	Stats      Stats
	// Error is the failure text of a failed run, otherwise empty.
	Error string
}
