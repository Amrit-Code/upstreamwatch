package model

import "time"

// FindingKind says what a finding is about, as stored in findings.kind.
type FindingKind string

// The stored values of FindingKind. A stored value is never renamed.
const (
	KindAdvisory   FindingKind = "advisory"
	KindIssue      FindingKind = "issue"
	KindJobFailure FindingKind = "job_failure"
)

// FindingStateValue is the operator-facing state of a finding, as stored in
// finding_state.state.
type FindingStateValue string

// The stored values of FindingStateValue. A stored value is never renamed.
const (
	StateOpen      FindingStateValue = "open"
	StateAcked     FindingStateValue = "acked"
	StateSnoozed   FindingStateValue = "snoozed"
	StateDismissed FindingStateValue = "dismissed"
)

// Finding is what the operator sees: an advisory match, a judged upstream
// issue, or a failed job. The ID fields that do not apply to its kind are zero;
// a job_failure finding has none of them.
type Finding struct {
	ID           int64
	Kind         FindingKind
	RepoID       int64
	PackageID    int64
	DepVersionID int64
	AdvisoryID   string
	IssueID      int64
	VerdictID    int64
	// Fingerprint identifies the finding across runs and is unique.
	Fingerprint string
	Score       float64
	RankReason  string
	// Times are UTC; a zero time means "not set".
	FirstSeenAt        time.Time
	LastChangedAt      time.Time
	ResolvedAt         time.Time
	ReportedInDigestAt time.Time
	State              FindingState
}

// FindingState is the current operator state of one finding.
type FindingState struct {
	State       FindingStateValue
	SnoozeUntil time.Time
	Note        string
	ChangedAt   time.Time
	ChangedBy   string
}
