package report

import (
	"context"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Notifier sends a digest to one channel: email, a Discord or Slack webhook,
// ntfy or stdout.
type Notifier interface {
	// Send delivers d. An error means the channel did not accept it.
	Send(ctx context.Context, d model.Digest) error
}

// Store is what the digest job reads and writes.
type Store interface {
	// ListFindings returns the unresolved findings that pass f, highest
	// score first, then lowest ID.
	ListFindings(ctx context.Context, f model.FindingFilter) ([]model.Finding, error)
	// GetFindingDetail returns the finding with this ID and what is needed
	// to show it, or an error wrapping model.ErrNotFound.
	GetFindingDetail(ctx context.Context, id int64) (model.FindingDetail, error)
	// MarkReported sets reported_in_digest_ts = at on the findings with
	// these IDs. It does not change last_changed_ts.
	MarkReported(ctx context.Context, ids []int64, at time.Time) error
	// LatestRuns returns the most recent run of each job, ordered by job
	// name.
	LatestRuns(ctx context.Context) ([]model.Run, error)
	// SaveDigest inserts rec and returns it with its ID set.
	SaveDigest(ctx context.Context, rec model.DigestRecord) (model.DigestRecord, error)
	// FailedDigests returns the rows with status failed, oldest first.
	FailedDigests(ctx context.Context) ([]model.DigestRecord, error)
	// MarkDigestRetried sets the status of the row with this ID to retried.
	MarkDigestRetried(ctx context.Context, id int64) error
	// LastSentDigest returns the most recent row with status sent, on any
	// channel, or an error wrapping model.ErrNotFound when there is none.
	LastSentDigest(ctx context.Context) (model.DigestRecord, error)
}
