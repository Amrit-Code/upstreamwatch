package findings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

type savedState struct {
	id int64
	st model.FindingState
}

// fakeStore holds findings by ID, records every saved state and can be told
// to fail a save.
type fakeStore struct {
	findings map[int64]model.Finding
	saveErr  error
	saved    []savedState
}

func (f *fakeStore) GetFinding(ctx context.Context, id int64) (model.Finding, error) {
	fd, ok := f.findings[id]
	if !ok {
		return model.Finding{}, fmt.Errorf("finding %d: %w", id, model.ErrNotFound)
	}
	return fd, nil
}

func (f *fakeStore) SaveFindingState(ctx context.Context, id int64, st model.FindingState) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, savedState{id: id, st: st})
	return nil
}

func newFixture() (*fakeStore, *Service) {
	store := &fakeStore{findings: map[int64]model.Finding{
		7: {ID: 7, Kind: model.KindJobFailure, State: model.FindingState{State: model.StateOpen}},
		9: {ID: 9, Kind: model.KindJobFailure, ResolvedAt: t0.Add(-time.Hour), State: model.FindingState{State: model.StateOpen}},
	}}
	return store, NewService(store, func() time.Time { return t0 })
}

// checkSaved fails the test unless store saved exactly want for finding 7.
func checkSaved(t *testing.T, store *fakeStore, want model.FindingState) {
	t.Helper()
	if len(store.saved) != 1 {
		t.Fatalf("saved %d states, want 1: %+v", len(store.saved), store.saved)
	}
	if got := store.saved[0]; got.id != 7 || got.st != want {
		t.Errorf("saved %+v, want id 7 state %+v", got, want)
	}
}

func TestAck(t *testing.T) {
	t.Parallel()
	store, svc := newFixture()

	got, err := svc.Ack(t.Context(), 7, "  looking into it ", "you")
	if err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	want := model.FindingState{State: model.StateAcked, Note: "looking into it", ChangedAt: t0, ChangedBy: "you"}
	if got != want {
		t.Errorf("Ack() = %+v, want %+v", got, want)
	}
	checkSaved(t, store, want)
}

func TestAck_NotFound(t *testing.T) {
	t.Parallel()
	store, svc := newFixture()

	_, err := svc.Ack(t.Context(), 404, "", "you")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("Ack(404) error = %v, want model.ErrNotFound", err)
	}
	if len(store.saved) != 0 {
		t.Errorf("saved %+v, want nothing", store.saved)
	}
}

func TestAck_Resolved(t *testing.T) {
	t.Parallel()
	store, svc := newFixture()

	_, err := svc.Ack(t.Context(), 9, "", "you")
	if !errors.Is(err, model.ErrInvalidTransition) {
		t.Errorf("Ack(resolved) error = %v, want model.ErrInvalidTransition", err)
	}
	if len(store.saved) != 0 {
		t.Errorf("saved %+v, want nothing", store.saved)
	}
}

func TestAck_BadNote(t *testing.T) {
	t.Parallel()
	store, svc := newFixture()

	_, err := svc.Ack(t.Context(), 7, strings.Repeat("x", model.MaxNoteRunes+1), "you")
	if !errors.Is(err, model.ErrInvalidTransition) {
		t.Errorf("Ack(long note) error = %v, want model.ErrInvalidTransition", err)
	}
	if len(store.saved) != 0 {
		t.Errorf("saved %+v, want nothing", store.saved)
	}
}

func TestAck_SaveFails(t *testing.T) {
	t.Parallel()
	store, svc := newFixture()
	store.saveErr = fmt.Errorf("save finding state 7: %w", model.ErrNotFound)

	got, err := svc.Ack(t.Context(), 7, "", "you")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("Ack() error = %v, want the store's error", err)
	}
	if got != (model.FindingState{}) {
		t.Errorf("Ack() = %+v, want the zero state on error", got)
	}
}

func TestSnooze(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		d       time.Duration
		wantErr bool
	}{
		{name: "1d", d: 24 * time.Hour},
		{name: "7d", d: 7 * 24 * time.Hour},
		{name: "30d", d: 30 * 24 * time.Hour},
		{name: "2h", d: 2 * time.Hour, wantErr: true},
		{name: "zero", d: 0, wantErr: true},
		{name: "negative 1d", d: -24 * time.Hour, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store, svc := newFixture()

			got, err := svc.Snooze(t.Context(), 7, tt.d, "after the release", "you")
			if tt.wantErr {
				if !errors.Is(err, model.ErrInvalidTransition) {
					t.Errorf("Snooze(%s) error = %v, want model.ErrInvalidTransition", tt.d, err)
				}
				if len(store.saved) != 0 {
					t.Errorf("saved %+v, want nothing", store.saved)
				}
				return
			}
			if err != nil {
				t.Fatalf("Snooze(%s) error = %v", tt.d, err)
			}
			want := model.FindingState{
				State:       model.StateSnoozed,
				SnoozeUntil: t0.Add(tt.d),
				Note:        "after the release",
				ChangedAt:   t0,
				ChangedBy:   "you",
			}
			if got != want {
				t.Errorf("Snooze(%s) = %+v, want %+v", tt.d, got, want)
			}
			checkSaved(t, store, want)
		})
	}
}

func TestSnooze_Resolved(t *testing.T) {
	t.Parallel()
	store, svc := newFixture()

	_, err := svc.Snooze(t.Context(), 9, 24*time.Hour, "", "you")
	if !errors.Is(err, model.ErrInvalidTransition) {
		t.Errorf("Snooze(resolved) error = %v, want model.ErrInvalidTransition", err)
	}
	if len(store.saved) != 0 {
		t.Errorf("saved %+v, want nothing", store.saved)
	}
}

func TestDismiss(t *testing.T) {
	t.Parallel()
	store, svc := newFixture()

	got, err := svc.Dismiss(t.Context(), 7, "not used here", "you")
	if err != nil {
		t.Fatalf("Dismiss() error = %v", err)
	}
	want := model.FindingState{State: model.StateDismissed, Note: "not used here", ChangedAt: t0, ChangedBy: "you"}
	if got != want {
		t.Errorf("Dismiss() = %+v, want %+v", got, want)
	}
	checkSaved(t, store, want)
}

func TestDismiss_NotFound(t *testing.T) {
	t.Parallel()
	_, svc := newFixture()

	if _, err := svc.Dismiss(t.Context(), 404, "", "you"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("Dismiss(404) error = %v, want model.ErrNotFound", err)
	}
}

func TestReopen(t *testing.T) {
	t.Parallel()
	store, svc := newFixture()
	f := store.findings[7]
	f.State = model.FindingState{State: model.StateAcked, Note: "old note", ChangedAt: t0.Add(-time.Hour), ChangedBy: "you"}
	store.findings[7] = f

	got, err := svc.Reopen(t.Context(), 7, "you")
	if err != nil {
		t.Fatalf("Reopen() error = %v", err)
	}
	want := model.FindingState{State: model.StateOpen, ChangedAt: t0, ChangedBy: "you"}
	if got != want {
		t.Errorf("Reopen() = %+v, want %+v", got, want)
	}
	checkSaved(t, store, want)
}

func TestReopen_Resolved(t *testing.T) {
	t.Parallel()
	store, svc := newFixture()

	if _, err := svc.Reopen(t.Context(), 9, "you"); !errors.Is(err, model.ErrInvalidTransition) {
		t.Errorf("Reopen(resolved) error = %v, want model.ErrInvalidTransition", err)
	}
	if len(store.saved) != 0 {
		t.Errorf("saved %+v, want nothing", store.saved)
	}
}

func TestNewService_NilClock(t *testing.T) {
	t.Parallel()
	store := &fakeStore{findings: map[int64]model.Finding{7: {ID: 7}}}
	svc := NewService(store, nil)

	got, err := svc.Ack(t.Context(), 7, "", "you")
	if err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if got.ChangedAt.IsZero() {
		t.Error("Ack() ChangedAt is zero, want the time from time.Now")
	}
}
