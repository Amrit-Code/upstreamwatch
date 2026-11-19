package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const digestColumns = `id, channel, status, sent_ts, payload, error`

// scanDigest reads one digests row selected with digestColumns.
func scanDigest(s scanner) (model.DigestRecord, error) {
	var (
		d                     model.DigestRecord
		channel, status, sent sql.NullString
		payload, errText      sql.NullString
	)
	if err := s.Scan(&d.ID, &channel, &status, &sent, &payload, &errText); err != nil {
		return model.DigestRecord{}, err
	}
	d.Channel = channel.String
	d.Status = model.DigestStatus(status.String)
	if payload.String != "" {
		d.Payload = []byte(payload.String)
	}
	d.Error = errText.String
	var err error
	if d.SentAt, err = parseTime(sent); err != nil {
		return model.DigestRecord{}, fmt.Errorf("digest %d sent_ts: %w", d.ID, err)
	}
	return d, nil
}

// SaveDigest inserts rec (channel, status, the attempt time from rec.SentAt,
// the payload as text and the error, NULL when empty) and returns it with its
// ID set and SentAt as stored: UTC, whole seconds.
func (db *DB) SaveDigest(ctx context.Context, rec model.DigestRecord) (model.DigestRecord, error) {
	now := formatTime(db.now())
	var id int64
	err := db.write(ctx, func() error {
		return db.q.QueryRowContext(ctx,
			`INSERT INTO digests (channel, status, sent_ts, payload, error, created_ts, updated_ts)
			 VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			rec.Channel, string(rec.Status), formatTime(rec.SentAt), string(rec.Payload),
			nullText(rec.Error), now, now).Scan(&id)
	})
	if err != nil {
		return model.DigestRecord{}, fmt.Errorf("save digest for channel %q: %w", rec.Channel, err)
	}
	rec.ID = id
	if !rec.SentAt.IsZero() {
		rec.SentAt = rec.SentAt.UTC().Truncate(time.Second)
	}
	return rec, nil
}

// FailedDigests returns the rows with status failed, ordered by ID (oldest
// first). It returns nil when there are none.
func (db *DB) FailedDigests(ctx context.Context) ([]model.DigestRecord, error) {
	rows, err := db.q.QueryContext(ctx,
		`SELECT `+digestColumns+` FROM digests WHERE status = ? ORDER BY id`,
		string(model.DigestFailed))
	if err != nil {
		return nil, fmt.Errorf("failed digests: %w", err)
	}
	defer rows.Close()
	var recs []model.DigestRecord
	for rows.Next() {
		d, err := scanDigest(rows)
		if err != nil {
			return nil, fmt.Errorf("failed digests: %w", err)
		}
		recs = append(recs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed digests: %w", err)
	}
	return recs, nil
}

// MarkDigestRetried sets the status of the row with this ID to retried. An
// unknown ID is an error wrapping model.ErrNotFound.
func (db *DB) MarkDigestRetried(ctx context.Context, id int64) error {
	var n int64
	err := db.write(ctx, func() error {
		res, err := db.q.ExecContext(ctx,
			`UPDATE digests SET status = ?, updated_ts = ? WHERE id = ?`,
			string(model.DigestRetried), formatTime(db.now()), id)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return fmt.Errorf("mark digest %d retried: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("mark digest %d retried: %w", id, model.ErrNotFound)
	}
	return nil
}

// LastSentDigest returns the row with status sent and the latest attempt
// time, on any channel (the highest ID among rows sent at the same second),
// or an error wrapping model.ErrNotFound when no row was sent.
func (db *DB) LastSentDigest(ctx context.Context) (model.DigestRecord, error) {
	d, err := scanDigest(db.q.QueryRowContext(ctx,
		`SELECT `+digestColumns+` FROM digests WHERE status = ?
		 ORDER BY sent_ts DESC, id DESC LIMIT 1`,
		string(model.DigestSent)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.DigestRecord{}, fmt.Errorf("last sent digest: %w", model.ErrNotFound)
	}
	if err != nil {
		return model.DigestRecord{}, fmt.Errorf("last sent digest: %w", err)
	}
	return d, nil
}
