package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestDB_StartFinishRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		finish    model.Run // ID filled in from StartRun
		wantStats string
		want      model.Stats
	}{
		{
			name:      "succeeded with stats",
			finish:    model.Run{Status: model.RunSucceeded, Stats: model.Stats{"fetched": 12, "not_modified": 3}},
			wantStats: `{"fetched":12,"not_modified":3}`,
			want:      model.Stats{"fetched": 12, "not_modified": 3},
		},
		{
			name:      "failed with nil stats",
			finish:    model.Run{Status: model.RunFailed, Error: "poll example-org/example-server: boom"},
			wantStats: `{}`,
		},
		{
			name:      "empty stats stored as object",
			finish:    model.Run{Status: model.RunSucceeded, Stats: model.Stats{}},
			wantStats: `{}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			ctx := t.Context()

			started, err := db.StartRun(ctx, "issues", t0)
			if err != nil {
				t.Fatalf("StartRun() error = %v", err)
			}
			if started.ID == 0 || started.Job != "issues" || !started.StartedAt.Equal(t0) ||
				started.Status != model.RunRunning || !started.FinishedAt.IsZero() || started.Stats != nil || started.Error != "" {
				t.Errorf("StartRun() = %+v, want a running run of issues at %v with an ID", started, t0)
			}
			running, err := db.LatestRuns(ctx)
			if err != nil || len(running) != 1 || !reflect.DeepEqual(running[0], started) {
				t.Errorf("LatestRuns() while running = %+v, %v; want [%+v]", running, err, started)
			}

			fin := started
			fin.FinishedAt = t0.Add(90 * time.Second)
			fin.Status, fin.Stats, fin.Error = tc.finish.Status, tc.finish.Stats, tc.finish.Error
			if err := db.FinishRun(ctx, fin); err != nil {
				t.Fatalf("FinishRun() error = %v", err)
			}

			var stats string
			if err := db.pool.QueryRowContext(ctx, "SELECT stats FROM runs WHERE id = ?", started.ID).Scan(&stats); err != nil {
				t.Fatalf("read stats: %v", err)
			}
			if stats != tc.wantStats {
				t.Errorf("stored stats = %s, want %s", stats, tc.wantStats)
			}

			got, err := db.LatestRuns(ctx)
			if err != nil {
				t.Fatalf("LatestRuns() error = %v", err)
			}
			want := fin
			want.Stats = tc.want
			if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
				t.Errorf("LatestRuns() = %+v, want [%+v]", got, want)
			}
		})
	}
}

func TestDB_FinishRun_NotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	err := db.FinishRun(t.Context(), model.Run{ID: 777, Status: model.RunSucceeded, FinishedAt: t0})
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("FinishRun(unknown ID) error = %v, want model.ErrNotFound", err)
	}
}

func TestDB_FinishRun_RejectsUnknownStatus(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	r, err := db.StartRun(t.Context(), "ingest", t0)
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	r.Status = "paused"
	if err := db.FinishRun(t.Context(), r); err == nil {
		t.Errorf("FinishRun(status paused) error = nil, want the CHECK to refuse it")
	}
}

func TestDB_LatestRuns(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		got, err := db.LatestRuns(t.Context())
		if err != nil || got != nil {
			t.Errorf("LatestRuns() = %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("latest per job ordered by name", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		ctx := t.Context()
		// Interleaved so neither insertion order nor start time decides.
		starts := []struct {
			job string
			at  time.Time
		}{
			{"timeline", t0},
			{"ingest", t0.Add(time.Hour)},
			{"vulns", t0.Add(2 * time.Hour)},
			{"ingest", t0.Add(-time.Hour)}, // higher ID, earlier start: still the latest
			{"timeline", t0.Add(3 * time.Hour)},
			{"vulns", t0.Add(4 * time.Hour)},
			{"timeline", t0.Add(5 * time.Hour)},
		}
		latest := map[string]model.Run{}
		for _, s := range starts {
			r, err := db.StartRun(ctx, s.job, s.at)
			if err != nil {
				t.Fatalf("StartRun(%s) error = %v", s.job, err)
			}
			latest[s.job] = r
		}
		got, err := db.LatestRuns(ctx)
		if err != nil {
			t.Fatalf("LatestRuns() error = %v", err)
		}
		want := []model.Run{latest["ingest"], latest["timeline"], latest["vulns"]}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("LatestRuns() = %+v, want %+v", got, want)
		}
	})
}
