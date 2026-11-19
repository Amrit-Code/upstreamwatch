package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// StateChange is one operator action on a finding.
type StateChange struct {
	To          FindingStateValue
	Now         time.Time
	SnoozeUntil time.Time // required, and after Now, when To is StateSnoozed
	Note        string
	By          string
}

// ErrInvalidTransition reports a state change that is not allowed: the
// finding is resolved, the target state is unknown, a snooze has no end in the
// future, or the note is not acceptable.
var ErrInvalidTransition = errors.New("invalid finding state change")

// MaxNoteRunes is the longest note accepted.
const MaxNoteRunes = 2000

// Transition returns the state f moves to under c, or an error wrapping
// ErrInvalidTransition.
//
// An unresolved finding may move from any state to open, acked, snoozed or
// dismissed, including the state it is in (re-acking replaces the note,
// re-snoozing moves the end). The note is trimmed of surrounding white space
// and then checked: valid UTF-8, at most MaxNoteRunes runes, and no control
// characters other than newline and tab. The returned state's times are UTC;
// SnoozeUntil is set only when the new state is snoozed.
func (f Finding) Transition(c StateChange) (FindingState, error) {
	if !f.ResolvedAt.IsZero() {
		return FindingState{}, fmt.Errorf("finding %d is resolved: %w", f.ID, ErrInvalidTransition)
	}
	var until time.Time
	switch c.To {
	case StateOpen, StateAcked, StateDismissed:
	case StateSnoozed:
		if c.SnoozeUntil.IsZero() {
			return FindingState{}, fmt.Errorf("snooze has no end: %w", ErrInvalidTransition)
		}
		if !c.SnoozeUntil.After(c.Now) {
			return FindingState{}, fmt.Errorf("snooze end %s is not after %s: %w",
				c.SnoozeUntil.UTC().Format(time.RFC3339), c.Now.UTC().Format(time.RFC3339), ErrInvalidTransition)
		}
		until = c.SnoozeUntil.UTC()
	default:
		return FindingState{}, fmt.Errorf("unknown state %q: %w", c.To, ErrInvalidTransition)
	}
	note := strings.TrimSpace(c.Note)
	if err := checkNote(note); err != nil {
		return FindingState{}, err
	}
	return FindingState{
		State:       c.To,
		SnoozeUntil: until,
		Note:        note,
		ChangedAt:   c.Now.UTC(),
		ChangedBy:   c.By,
	}, nil
}

// checkNote returns an error wrapping ErrInvalidTransition when note is not
// valid UTF-8, is longer than MaxNoteRunes runes, or holds a control character
// other than newline and tab.
func checkNote(note string) error {
	if !utf8.ValidString(note) {
		return fmt.Errorf("note is not valid UTF-8: %w", ErrInvalidTransition)
	}
	if n := utf8.RuneCountInString(note); n > MaxNoteRunes {
		return fmt.Errorf("note has %d characters, more than %d: %w", n, MaxNoteRunes, ErrInvalidTransition)
	}
	for _, r := range note {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return fmt.Errorf("note contains control character %U: %w", r, ErrInvalidTransition)
		}
	}
	return nil
}

// Effective returns the state as the operator sees it at now: a snooze that
// has ended counts as open.
func (s FindingState) Effective(now time.Time) FindingStateValue {
	if s.State == StateSnoozed && !s.SnoozeUntil.After(now) {
		return StateOpen
	}
	return s.State
}
