package store

import (
	"errors"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
)

var _ vuln.KVStore = (*DB)(nil)

func TestKV_GetSet(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	const key = "osv.modified.Go"

	_, err := db.GetKV(ctx, key)
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("GetKV(missing) error = %v, want model.ErrNotFound", err)
	}

	if err := db.SetKV(ctx, key, "2026-03-01T00:00:00Z"); err != nil {
		t.Fatalf("SetKV() error = %v", err)
	}
	if got, err := db.GetKV(ctx, key); err != nil || got != "2026-03-01T00:00:00Z" {
		t.Errorf("GetKV() = %q, %v, want %q, nil", got, err, "2026-03-01T00:00:00Z")
	}

	later := t0.Add(time.Hour)
	db.now = func() time.Time { return later }
	if err := db.SetKV(ctx, key, ""); err != nil {
		t.Fatalf("second SetKV() error = %v", err)
	}
	if got, err := db.GetKV(ctx, key); err != nil || got != "" {
		t.Errorf("GetKV() after replace = %q, %v, want empty, nil", got, err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM kv WHERE key = ? AND updated_ts = ?`, key, "2026-03-01T13:00:00Z"); n != 1 {
		t.Errorf("kv rows with the new updated_ts = %d, want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM kv`); n != 1 {
		t.Errorf("kv rows = %d, want 1", n)
	}
}

func TestKV_SetJoinsCallerTransaction(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	errStop := errors.New("stop")
	err := db.InTx(ctx, func(tx *DB) error {
		if err := tx.SetKV(ctx, "k", "v"); err != nil {
			return err
		}
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("InTx() error = %v, want %v", err, errStop)
	}
	if _, err := db.GetKV(ctx, "k"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetKV() after rollback error = %v, want model.ErrNotFound", err)
	}
}
