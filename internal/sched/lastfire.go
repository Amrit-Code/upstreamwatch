package sched

import (
	"fmt"
	"time"
)

// lastFireWindow is how far back LastFire looks for a fire time.
const lastFireWindow = 35 * 24 * time.Hour

// LastFire returns the most recent time at or before now at which the
// five-field cron expression fires, searching back at most 35 days; ok is
// false when it does not fire in that window.
//
// The expression is parsed as the Scheduler parses it, so a seconds field,
// a descriptor such as @weekly or a time zone prefix is an error. It is
// evaluated in now's location.
func LastFire(expr string, now time.Time) (t time.Time, ok bool, err error) {
	schedule, err := parseFiveField(expr)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse cron expression %q: %w", expr, err)
	}
	// Next returns the first fire strictly after its argument, at a whole
	// second, so starting one second early lets a fire exactly at the start
	// of the window count.
	start := now.Add(-lastFireWindow).Truncate(time.Second)
	fire := schedule.Next(start.Add(-time.Second))
	if fire.IsZero() || fire.After(now) {
		return time.Time{}, false, nil
	}
	for {
		next := schedule.Next(fire)
		if next.IsZero() || next.After(now) {
			return fire, true, nil
		}
		fire = next
	}
}
