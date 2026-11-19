package model

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTransition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	later := now.Add(7 * 24 * time.Hour)
	states := []FindingStateValue{StateOpen, StateAcked, StateSnoozed, StateDismissed}

	// Every allowed move: each state to each state, including itself.
	for _, from := range states {
		for _, to := range states {
			t.Run(string(from)+" to "+string(to), func(t *testing.T) {
				t.Parallel()
				f := Finding{ID: 7, State: FindingState{State: from, SnoozeUntil: now.Add(time.Hour), Note: "old"}}
				c := StateChange{To: to, Now: now, Note: "  looked at it\n\tfine  ", By: "operator"}
				want := FindingState{State: to, Note: "looked at it\n\tfine", ChangedAt: now, ChangedBy: "operator"}
				if to == StateSnoozed {
					c.SnoozeUntil = later
					want.SnoozeUntil = later
				}
				got, err := f.Transition(c)
				if err != nil {
					t.Fatalf("Transition(%+v) error = %v, want nil", c, err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("Transition(%+v) = %+v, want %+v", c, got, want)
				}
			})
		}
	}

	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load Europe/London: %v", err)
	}
	t.Run("times are returned in UTC", func(t *testing.T) {
		t.Parallel()
		localNow := time.Date(2026, 7, 1, 13, 0, 0, 0, london)
		localUntil := time.Date(2026, 7, 2, 13, 0, 0, 0, london)
		got, err := Finding{}.Transition(StateChange{To: StateSnoozed, Now: localNow, SnoozeUntil: localUntil})
		if err != nil {
			t.Fatalf("Transition() error = %v", err)
		}
		if got.ChangedAt.Location() != time.UTC || !got.ChangedAt.Equal(localNow) {
			t.Errorf("ChangedAt = %v, want %v in UTC", got.ChangedAt, localNow.UTC())
		}
		if got.SnoozeUntil.Location() != time.UTC || !got.SnoozeUntil.Equal(localUntil) {
			t.Errorf("SnoozeUntil = %v, want %v in UTC", got.SnoozeUntil, localUntil.UTC())
		}
	})

	t.Run("note at the limit is accepted", func(t *testing.T) {
		t.Parallel()
		note := strings.Repeat("é", MaxNoteRunes)
		got, err := Finding{}.Transition(StateChange{To: StateAcked, Now: now, Note: note})
		if err != nil {
			t.Fatalf("Transition() error = %v, want nil", err)
		}
		if got.Note != note {
			t.Errorf("Note changed: got %d runes", len([]rune(got.Note)))
		}
	})

	errTests := []struct {
		name string
		f    Finding
		c    StateChange
	}{
		{"resolved", Finding{ResolvedAt: now.Add(-time.Hour)}, StateChange{To: StateAcked, Now: now}},
		{"unknown state", Finding{}, StateChange{To: "closed", Now: now}},
		{"empty state", Finding{}, StateChange{Now: now}},
		{"snooze without end", Finding{}, StateChange{To: StateSnoozed, Now: now}},
		{"snooze end in the past", Finding{}, StateChange{To: StateSnoozed, Now: now, SnoozeUntil: now.Add(-time.Minute)}},
		{"snooze end equal to now", Finding{}, StateChange{To: StateSnoozed, Now: now, SnoozeUntil: now}},
		{"note too long", Finding{}, StateChange{To: StateAcked, Now: now, Note: strings.Repeat("a", MaxNoteRunes+1)}},
		{"note with escape", Finding{}, StateChange{To: StateAcked, Now: now, Note: "ok \x1b[31m red"}},
		{"note with carriage return inside", Finding{}, StateChange{To: StateAcked, Now: now, Note: "a\rb"}},
		{"note with C1 control", Finding{}, StateChange{To: StateAcked, Now: now, Note: "a\u0085b"}},
		{"note not UTF-8", Finding{}, StateChange{To: StateDismissed, Now: now, Note: "bad \xff byte"}},
	}
	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.f.Transition(tt.c)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Transition(%+v) error = %v, want ErrInvalidTransition", tt.c, err)
			}
			if !reflect.DeepEqual(got, FindingState{}) {
				t.Errorf("Transition(%+v) state = %+v, want zero on error", tt.c, got)
			}
		})
	}
}

func TestEffective(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		s    FindingState
		want FindingStateValue
	}{
		{"snoozed until the past", FindingState{State: StateSnoozed, SnoozeUntil: now.Add(-time.Second)}, StateOpen},
		{"snoozed until now", FindingState{State: StateSnoozed, SnoozeUntil: now}, StateOpen},
		{"snoozed until the future", FindingState{State: StateSnoozed, SnoozeUntil: now.Add(time.Second)}, StateSnoozed},
		{"snoozed without end", FindingState{State: StateSnoozed}, StateOpen},
		{"open", FindingState{State: StateOpen}, StateOpen},
		{"acked", FindingState{State: StateAcked, SnoozeUntil: now.Add(-time.Hour)}, StateAcked},
		{"dismissed", FindingState{State: StateDismissed}, StateDismissed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.s.Effective(now); got != tt.want {
				t.Errorf("%+v.Effective(%v) = %q, want %q", tt.s, now, got, tt.want)
			}
		})
	}
}
