package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// GetKV returns the value stored under key, or an error wrapping
// model.ErrNotFound.
func (db *DB) GetKV(ctx context.Context, key string) (string, error) {
	var value string
	err := db.q.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("kv %q: %w", key, model.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("get kv %q: %w", key, err)
	}
	return value, nil
}

// SetKV stores value under key, replacing any previous value, and sets the
// row's updated time.
func (db *DB) SetKV(ctx context.Context, key, value string) error {
	err := db.InTx(ctx, func(tx *DB) error {
		_, err := tx.q.ExecContext(ctx,
			`INSERT INTO kv (key, value, updated_ts) VALUES (?, ?, ?)
			 ON CONFLICT (key) DO UPDATE SET
				value = excluded.value,
				updated_ts = excluded.updated_ts`,
			key, value, formatTime(tx.now()))
		return err
	})
	if err != nil {
		return fmt.Errorf("set kv %q: %w", key, err)
	}
	return nil
}
