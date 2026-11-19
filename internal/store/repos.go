package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const repoColumns = `id, slug, url, default_branch, clone_path, last_indexed_sha,
	timeline_settings, last_fetch_ts, last_fetch_error`

// scanRepo reads one repos row selected with repoColumns.
func scanRepo(s scanner) (model.TargetRepo, error) {
	var (
		r                                 model.TargetRepo
		url, branch, clonePath, indexed   sql.NullString
		settings, lastFetch, lastFetchErr sql.NullString
	)
	if err := s.Scan(&r.ID, &r.Slug, &url, &branch, &clonePath, &indexed,
		&settings, &lastFetch, &lastFetchErr); err != nil {
		return model.TargetRepo{}, err
	}
	r.URL = url.String
	r.DefaultBranch = branch.String
	r.ClonePath = clonePath.String
	r.LastIndexedSHA = indexed.String
	r.TimelineSettings = settings.String
	r.LastFetchError = lastFetchErr.String
	var err error
	if r.LastFetchAt, err = parseTime(lastFetch); err != nil {
		return model.TargetRepo{}, fmt.Errorf("repo %d last_fetch_ts: %w", r.ID, err)
	}
	return r, nil
}

// UpsertRepo inserts a repos row for slug the first time it is seen and
// updates only its URL after that. It returns the stored repo with its ID set.
func (db *DB) UpsertRepo(ctx context.Context, slug, url string) (model.TargetRepo, error) {
	now := formatTime(db.now())
	r, err := scanRepo(db.q.QueryRowContext(ctx,
		`INSERT INTO repos (slug, url, created_ts, updated_ts)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (slug) DO UPDATE SET
			url = excluded.url,
			updated_ts = excluded.updated_ts
		 RETURNING `+repoColumns,
		slug, nullText(url), now, now))
	if err != nil {
		return model.TargetRepo{}, fmt.Errorf("upsert repo %q: %w", slug, err)
	}
	return r, nil
}

// RecordFetch stores one fetch attempt of r.RepoID: the attempt time and error
// text always (the error as NULL when r.Error is empty), and the default
// branch and clone path only when the attempt succeeded. An unknown repo ID is
// an error wrapping model.ErrNotFound.
func (db *DB) RecordFetch(ctx context.Context, r model.FetchResult) error {
	var (
		res sql.Result
		err error
	)
	if r.Error == "" {
		res, err = db.q.ExecContext(ctx,
			`UPDATE repos SET last_fetch_ts = ?, last_fetch_error = NULL,
				default_branch = ?, clone_path = ?, updated_ts = ?
			 WHERE id = ?`,
			formatTime(r.At), nullText(r.Branch), nullText(r.ClonePath), formatTime(db.now()), r.RepoID)
	} else {
		res, err = db.q.ExecContext(ctx,
			`UPDATE repos SET last_fetch_ts = ?, last_fetch_error = ?, updated_ts = ?
			 WHERE id = ?`,
			formatTime(r.At), r.Error, formatTime(db.now()), r.RepoID)
	}
	if err != nil {
		return fmt.Errorf("record fetch of repo %d: %w", r.RepoID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("record fetch of repo %d: %w", r.RepoID, err)
	}
	if n == 0 {
		return fmt.Errorf("record fetch of repo %d: %w", r.RepoID, model.ErrNotFound)
	}
	return nil
}

// GetRepo returns the repo stored under slug, or an error wrapping
// model.ErrNotFound.
func (db *DB) GetRepo(ctx context.Context, slug string) (model.TargetRepo, error) {
	r, err := scanRepo(db.q.QueryRowContext(ctx,
		`SELECT `+repoColumns+` FROM repos WHERE slug = ?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return model.TargetRepo{}, fmt.Errorf("repo %q: %w", slug, model.ErrNotFound)
	}
	if err != nil {
		return model.TargetRepo{}, fmt.Errorf("get repo %q: %w", slug, err)
	}
	return r, nil
}

// ListRepos returns every stored repo ordered by slug. It returns nil when
// there are none.
func (db *DB) ListRepos(ctx context.Context) ([]model.TargetRepo, error) {
	rows, err := db.q.QueryContext(ctx, `SELECT `+repoColumns+` FROM repos ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()
	var repos []model.TargetRepo
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, fmt.Errorf("list repos: %w", err)
		}
		repos = append(repos, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	return repos, nil
}
