package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const httpValidatorColumns = `url, etag, last_modified, fetched_ts, status`

// scanHTTPValidator reads one http_cache row selected with httpValidatorColumns.
func scanHTTPValidator(s scanner) (model.HTTPValidator, error) {
	var (
		v                  model.HTTPValidator
		etag, lastModified sql.NullString
		fetched            sql.NullString
		status             sql.NullInt64
	)
	if err := s.Scan(&v.URL, &etag, &lastModified, &fetched, &status); err != nil {
		return model.HTTPValidator{}, err
	}
	v.ETag, v.LastModified = etag.String, lastModified.String
	v.Status = int(status.Int64)
	var err error
	if v.FetchedAt, err = parseTime(fetched); err != nil {
		return model.HTTPValidator{}, fmt.Errorf("http validator fetched_ts: %w", err)
	}
	return v, nil
}

// urlHost returns the host of raw for error messages, which must not carry the
// path or query: a feed URL's query can hold an API key.
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// GetHTTPValidator returns the validators stored for the exact URL. No row is
// an error wrapping model.ErrNotFound that names the URL's host only.
func (db *DB) GetHTTPValidator(ctx context.Context, url string) (model.HTTPValidator, error) {
	v, err := scanHTTPValidator(db.q.QueryRowContext(ctx,
		`SELECT `+httpValidatorColumns+` FROM http_cache WHERE url = ?`, url))
	if errors.Is(err, sql.ErrNoRows) {
		return model.HTTPValidator{}, fmt.Errorf("http validator for host %q: %w", urlHost(url), model.ErrNotFound)
	}
	if err != nil {
		return model.HTTPValidator{}, fmt.Errorf("get http validator for host %q: %w", urlHost(url), err)
	}
	return v, nil
}

// SaveHTTPValidator stores v, replacing any validators held for v.URL. Empty
// ETag and LastModified are stored as NULL. An empty v.URL is an error.
func (db *DB) SaveHTTPValidator(ctx context.Context, v model.HTTPValidator) error {
	if v.URL == "" {
		return errors.New("save http validator: empty url")
	}
	err := db.write(ctx, func() error {
		_, err := db.q.ExecContext(ctx,
			`INSERT INTO http_cache (`+httpValidatorColumns+`, updated_ts)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT (url) DO UPDATE SET
				etag = excluded.etag,
				last_modified = excluded.last_modified,
				fetched_ts = excluded.fetched_ts,
				status = excluded.status,
				updated_ts = excluded.updated_ts`,
			v.URL, nullText(v.ETag), nullText(v.LastModified), formatTime(v.FetchedAt), v.Status,
			formatTime(db.now()))
		return err
	})
	if err != nil {
		return fmt.Errorf("save http validator for host %q: %w", urlHost(v.URL), err)
	}
	return nil
}
