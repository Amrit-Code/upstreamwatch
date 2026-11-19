package store

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestDigests(t *testing.T) {
	t.Parallel()

	t.Run("empty table", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		if _, err := db.LastSentDigest(t.Context()); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("LastSentDigest() on an empty table error = %v, want one wrapping model.ErrNotFound", err)
		}
		if got, err := db.FailedDigests(t.Context()); err != nil || got != nil {
			t.Errorf("FailedDigests() on an empty table = %v, %v; want nil, nil", got, err)
		}
		if err := db.MarkDigestRetried(t.Context(), 1); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("MarkDigestRetried(unknown) error = %v, want one wrapping model.ErrNotFound", err)
		}
	})

	t.Run("save, list failed, retry, last sent", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		ctx := t.Context()
		day1 := time.Date(2026, 3, 2, 7, 30, 0, 0, time.UTC)
		day2 := day1.Add(24 * time.Hour)
		save := func(rec model.DigestRecord) model.DigestRecord {
			t.Helper()
			got, err := db.SaveDigest(ctx, rec)
			if err != nil {
				t.Fatalf("SaveDigest(%s %s) error = %v", rec.Channel, rec.Status, err)
			}
			if got.ID == 0 {
				t.Fatalf("SaveDigest(%s %s) ID = 0, want the row ID", rec.Channel, rec.Status)
			}
			return got
		}
		payload := []byte(`{"generated_at":"2026-03-02T07:30:00Z","new":[]}`)
		// SentAt carries a fraction and a zone, to show what is stored.
		emailSent := save(model.DigestRecord{Channel: "email", Status: model.DigestSent,
			SentAt: day1.Add(500 * time.Millisecond).In(time.FixedZone("UTC+1", 3600)), Payload: payload})
		webhookFailed := save(model.DigestRecord{Channel: "discord", Status: model.DigestFailed,
			SentAt: day1, Payload: payload, Error: "post webhook: 502 Bad Gateway"})
		emailFailed := save(model.DigestRecord{Channel: "email", Status: model.DigestFailed,
			SentAt: day2, Payload: payload, Error: "smtp: connection refused"})
		webhookSent := save(model.DigestRecord{Channel: "discord", Status: model.DigestSent,
			SentAt: day2, Payload: payload})

		wantEmailSent := model.DigestRecord{ID: emailSent.ID, Channel: "email", Status: model.DigestSent,
			SentAt: day1, Payload: payload}
		if !reflect.DeepEqual(emailSent, wantEmailSent) {
			t.Errorf("SaveDigest() = %+v, want %+v", emailSent, wantEmailSent)
		}
		var errText sql.NullString
		var created, updated string
		if err := db.pool.QueryRowContext(ctx, "SELECT error, created_ts, updated_ts FROM digests WHERE id = ?", emailSent.ID).
			Scan(&errText, &created, &updated); err != nil {
			t.Fatalf("read digest row: %v", err)
		}
		if errText.Valid || created != ts0 || updated != ts0 {
			t.Errorf("stored error, created_ts, updated_ts = %v, %s, %s; want NULL, %s, %s", errText, created, updated, ts0, ts0)
		}

		failed, err := db.FailedDigests(ctx)
		if err != nil {
			t.Fatalf("FailedDigests() error = %v", err)
		}
		if want := []model.DigestRecord{webhookFailed, emailFailed}; !reflect.DeepEqual(failed, want) {
			t.Errorf("FailedDigests() =\n%+v\nwant\n%+v", failed, want)
		}

		// Both sent rows of day 2 share a second; the higher ID wins.
		last, err := db.LastSentDigest(ctx)
		if err != nil || !reflect.DeepEqual(last, webhookSent) {
			t.Errorf("LastSentDigest() = %+v, %v; want %+v", last, err, webhookSent)
		}
		retrySent := save(model.DigestRecord{Channel: "email", Status: model.DigestSent, SentAt: day2, Payload: payload})
		if last, err := db.LastSentDigest(ctx); err != nil || last.ID != retrySent.ID {
			t.Errorf("LastSentDigest() after a same-second send = ID %d, %v; want ID %d", last.ID, err, retrySent.ID)
		}

		db.now = func() time.Time { return t0.Add(time.Hour) }
		if err := db.MarkDigestRetried(ctx, emailFailed.ID); err != nil {
			t.Fatalf("MarkDigestRetried() error = %v", err)
		}
		failed, err = db.FailedDigests(ctx)
		if err != nil {
			t.Fatalf("FailedDigests() after retry error = %v", err)
		}
		if want := []model.DigestRecord{webhookFailed}; !reflect.DeepEqual(failed, want) {
			t.Errorf("FailedDigests() after retry =\n%+v\nwant\n%+v", failed, want)
		}
		var status string
		if err := db.pool.QueryRowContext(ctx, "SELECT status, updated_ts FROM digests WHERE id = ?", emailFailed.ID).
			Scan(&status, &updated); err != nil {
			t.Fatalf("read retried row: %v", err)
		}
		if status != string(model.DigestRetried) || updated != "2026-03-01T13:00:00Z" {
			t.Errorf("retried row status, updated_ts = %s, %s; want retried, 2026-03-01T13:00:00Z", status, updated)
		}
		// A retried row is never the last sent one.
		if last, err := db.LastSentDigest(ctx); err != nil || last.Status != model.DigestSent {
			t.Errorf("LastSentDigest() after retry = %+v, %v; want a sent row", last, err)
		}
	})

	t.Run("only failed rows count as failed", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		if _, err := db.SaveDigest(t.Context(), model.DigestRecord{Channel: "stdout", Status: model.DigestSent, SentAt: t0}); err != nil {
			t.Fatalf("SaveDigest() error = %v", err)
		}
		if got, err := db.FailedDigests(t.Context()); err != nil || got != nil {
			t.Errorf("FailedDigests() with only a sent row = %v, %v; want nil, nil", got, err)
		}
	})
}
