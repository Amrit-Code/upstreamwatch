package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const runColumns = `id, job, started_ts, finished_ts, status, stats, error`

// scanRun reads one runs row selected with runColumns.
func scanRun(s scanner) (model.Run, error) {
	var (
		r                 model.Run
		started, finished sql.NullString
		status, statsJSON string
	)
	if err := s.Scan(&r.ID, &r.Job, &started, &finished, &status, &statsJSON, &r.Error); err != nil {
		return model.Run{}, err
	}
	var err error
	if r.StartedAt, err = parseTime(started); err != nil {
		return model.Run{}, fmt.Errorf("run %d started_ts: %w", r.ID, err)
	}
	if r.FinishedAt, err = parseTime(finished); err != nil {
		return model.Run{}, fmt.Errorf("run %d finished_ts: %w", r.ID, err)
	}
	r.Status = model.RunStatus(status)
	if r.Stats, err = decodeStats(statsJSON); err != nil {
		return model.Run{}, fmt.Errorf("run %d stats: %w", r.ID, err)
	}
	return r, nil
}

// encodeStats renders stats as a JSON object; nil and empty become "{}".
func encodeStats(st model.Stats) (string, error) {
	if len(st) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeStats reads a stored JSON object; an empty object becomes nil, so an
// empty Stats round-trips as nil.
func decodeStats(s string) (model.Stats, error) {
	var st model.Stats
	if err := json.Unmarshal([]byte(s), &st); err != nil {
		return nil, err
	}
	if len(st) == 0 {
		return nil, nil
	}
	return st, nil
}

// StartRun inserts a run of job with status model.RunRunning and returns it
// with its ID set.
func (db *DB) StartRun(ctx context.Context, job string, startedAt time.Time) (model.Run, error) {
	now := formatTime(db.now())
	var id int64
	err := db.q.QueryRowContext(ctx,
		`INSERT INTO runs (job, started_ts, status, stats, error, created_ts, updated_ts)
		 VALUES (?, ?, ?, '{}', '', ?, ?) RETURNING id`,
		job, formatTime(startedAt), string(model.RunRunning), now, now).Scan(&id)
	if err != nil {
		return model.Run{}, fmt.Errorf("start run %q: %w", job, err)
	}
	return model.Run{
		ID:        id,
		Job:       job,
		StartedAt: startedAt.UTC().Truncate(time.Second),
		Status:    model.RunRunning,
	}, nil
}

// FinishRun stores FinishedAt, Status, Stats and Error for run.ID. An unknown
// ID is an error wrapping model.ErrNotFound.
func (db *DB) FinishRun(ctx context.Context, run model.Run) error {
	stats, err := encodeStats(run.Stats)
	if err != nil {
		return fmt.Errorf("finish run %d: encode stats: %w", run.ID, err)
	}
	res, err := db.q.ExecContext(ctx,
		`UPDATE runs SET finished_ts = ?, status = ?, stats = ?, error = ?, updated_ts = ? WHERE id = ?`,
		formatTime(run.FinishedAt), string(run.Status), stats, run.Error, formatTime(db.now()), run.ID)
	if err != nil {
		return fmt.Errorf("finish run %d: %w", run.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("finish run %d: %w", run.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("finish run %d: %w", run.ID, model.ErrNotFound)
	}
	return nil
}

// LatestRuns returns the most recent run (highest ID) of each job, ordered by
// job name. It returns nil when there are no runs.
func (db *DB) LatestRuns(ctx context.Context) ([]model.Run, error) {
	rows, err := db.q.QueryContext(ctx,
		`SELECT `+runColumns+` FROM runs
		 WHERE id IN (SELECT MAX(id) FROM runs GROUP BY job)
		 ORDER BY job`)
	if err != nil {
		return nil, fmt.Errorf("latest runs: %w", err)
	}
	defer rows.Close()
	var runs []model.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("latest runs: %w", err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("latest runs: %w", err)
	}
	return runs, nil
}
