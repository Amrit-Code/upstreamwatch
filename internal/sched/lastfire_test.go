package sched_test

import (
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/sched"
)

func TestLastFire(t *testing.T) {
	t.Parallel()
	// 2026-11-16 is a Monday.
	monday8 := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		expr   string
		now    time.Time
		want   time.Time
		wantOK bool
	}{
		{name: "fires exactly at now", expr: "0 8 * * 1", now: monday8, want: monday8, wantOK: true},
		{name: "a minute before the fire", expr: "0 8 * * 1", now: monday8.Add(-time.Minute),
			want: monday8.AddDate(0, 0, -7), wantOK: true},
		{name: "wednesday after", expr: "0 8 * * 1", now: time.Date(2026, 11, 18, 13, 30, 0, 0, time.UTC),
			want: monday8, wantOK: true},
		{name: "seconds past the fire", expr: "0 8 * * 1", now: monday8.Add(30*time.Second + 5*time.Millisecond),
			want: monday8, wantOK: true},
		{name: "every minute", expr: "* * * * *", now: time.Date(2026, 11, 18, 13, 30, 59, 0, time.UTC),
			want: time.Date(2026, 11, 18, 13, 30, 0, 0, time.UTC), wantOK: true},
		{name: "yearly, last fired 200 days ago", expr: "0 0 1 1 *", now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 200)},
		{name: "fire exactly 35 days back", expr: "0 8 12 10 *", now: time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC),
			want: time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC), wantOK: true},
		{name: "fire just over 35 days back", expr: "59 7 12 10 *", now: time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)},
		{name: "fire 35 days and half a second back", expr: "0 8 12 10 *",
			now: time.Date(2026, 11, 16, 8, 0, 0, 500_000_000, time.UTC)},
		{name: "fire 35 days and one nanosecond back", expr: "0 8 12 10 *",
			now: time.Date(2026, 11, 16, 8, 0, 0, 1, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok, err := sched.LastFire(tt.expr, tt.now)
			if err != nil {
				t.Fatalf("LastFire(%q, %v) error = %v", tt.expr, tt.now, err)
			}
			if ok != tt.wantOK || !got.Equal(tt.want) {
				t.Errorf("LastFire(%q, %v) = %v, %v; want %v, %v", tt.expr, tt.now, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestLastFire_RejectsWhatTheSchedulerRejects(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	for _, expr := range []string{"", "@weekly", "0 0 8 * * 1", "CRON_TZ=UTC 0 8 * * 1", "TZ=", "61 8 * * 1"} {
		t.Run(expr, func(t *testing.T) {
			t.Parallel()
			got, ok, err := sched.LastFire(expr, now)
			if err == nil {
				t.Errorf("LastFire(%q) = %v, %v, nil; want an error", expr, got, ok)
			}
			if ok || !got.IsZero() {
				t.Errorf("LastFire(%q) = %v, %v with an error; want zero time and false", expr, got, ok)
			}
		})
	}
}

func TestLastFire_UsesNowsLocation(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("UTC+2", 2*60*60)
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, loc)
	got, ok, err := sched.LastFire("0 8 * * 1", now)
	if err != nil || !ok {
		t.Fatalf("LastFire() = %v, %v, %v; want a fire", got, ok, err)
	}
	if want := time.Date(2026, 11, 16, 8, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("LastFire() = %v, want %v", got, want)
	}
}
