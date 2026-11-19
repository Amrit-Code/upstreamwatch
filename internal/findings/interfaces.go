package findings

import (
	"context"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Store reads a finding and writes its operator state.
type Store interface {
	// GetFinding returns the finding with this ID and its state, or an error
	// wrapping model.ErrNotFound.
	GetFinding(ctx context.Context, id int64) (model.Finding, error)
	// SaveFindingState replaces the finding_state row of the finding with
	// this ID with st (state, snooze_until_ts, note, changed_ts,
	// changed_by). An unknown ID is an error wrapping model.ErrNotFound.
	SaveFindingState(ctx context.Context, id int64, st model.FindingState) error
}
