package findings

import (
	"context"
	"fmt"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// The snooze lengths the operator may choose: one, seven and thirty days.
const (
	snoozeDay   = 24 * time.Hour
	snoozeWeek  = 7 * snoozeDay
	snoozeMonth = 30 * snoozeDay
)

// Service applies operator actions to a finding. Whether an action is allowed
// is decided by model.Finding.Transition; Service loads the finding, asks it,
// and stores the new state.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService returns a Service that reads and writes findings through store.
// now returns the current time; nil means time.Now.
func NewService(store Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}

// Ack marks the finding acknowledged with note, by the operator by, and
// returns its new state. A missing finding is an error wrapping
// model.ErrNotFound; a change the finding does not allow is an error wrapping
// model.ErrInvalidTransition, and nothing is saved.
func (s *Service) Ack(ctx context.Context, id int64, note, by string) (model.FindingState, error) {
	now := s.now()
	st, err := s.apply(ctx, id, model.StateChange{To: model.StateAcked, Now: now, Note: note, By: by})
	if err != nil {
		return model.FindingState{}, fmt.Errorf("ack finding %d: %w", id, err)
	}
	return st, nil
}

// Snooze hides the finding until now plus d and returns its new state. d must
// be one, seven or thirty days; any other length is an error wrapping
// model.ErrInvalidTransition. The other errors are those of Ack.
func (s *Service) Snooze(ctx context.Context, id int64, d time.Duration, note, by string) (model.FindingState, error) {
	switch d {
	case snoozeDay, snoozeWeek, snoozeMonth:
	default:
		return model.FindingState{}, fmt.Errorf("snooze finding %d: length %s is not 1, 7 or 30 days: %w",
			id, d, model.ErrInvalidTransition)
	}
	now := s.now()
	st, err := s.apply(ctx, id, model.StateChange{
		To:          model.StateSnoozed,
		Now:         now,
		SnoozeUntil: now.Add(d),
		Note:        note,
		By:          by,
	})
	if err != nil {
		return model.FindingState{}, fmt.Errorf("snooze finding %d: %w", id, err)
	}
	return st, nil
}

// Dismiss marks the finding dismissed with note and returns its new state. The
// errors are those of Ack.
func (s *Service) Dismiss(ctx context.Context, id int64, note, by string) (model.FindingState, error) {
	now := s.now()
	st, err := s.apply(ctx, id, model.StateChange{To: model.StateDismissed, Now: now, Note: note, By: by})
	if err != nil {
		return model.FindingState{}, fmt.Errorf("dismiss finding %d: %w", id, err)
	}
	return st, nil
}

// Reopen returns the finding to open with an empty note and returns its new
// state. The errors are those of Ack.
func (s *Service) Reopen(ctx context.Context, id int64, by string) (model.FindingState, error) {
	now := s.now()
	st, err := s.apply(ctx, id, model.StateChange{To: model.StateOpen, Now: now, By: by})
	if err != nil {
		return model.FindingState{}, fmt.Errorf("reopen finding %d: %w", id, err)
	}
	return st, nil
}

// apply loads the finding, computes its new state under c and saves it.
func (s *Service) apply(ctx context.Context, id int64, c model.StateChange) (model.FindingState, error) {
	f, err := s.store.GetFinding(ctx, id)
	if err != nil {
		return model.FindingState{}, err
	}
	st, err := f.Transition(c)
	if err != nil {
		return model.FindingState{}, err
	}
	if err := s.store.SaveFindingState(ctx, id, st); err != nil {
		return model.FindingState{}, err
	}
	return st, nil
}
