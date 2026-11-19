package store

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// newShortBusyDB opens and migrates a fresh database whose busy timeout is
// busy instead of five seconds, so a test can outlast it quickly.
func newShortBusyDB(t *testing.T, busy time.Duration) *DB {
	t.Helper()
	db, err := open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"), busy, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	db.now = func() time.Time { return t0 }
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return db
}

func TestWriteQueue_SingleWriteWaitsForLongTx(t *testing.T) {
	t.Parallel()

	t.Run("waits past the busy timeout and succeeds", func(t *testing.T) {
		t.Parallel()
		const busy = 200 * time.Millisecond
		db := newShortBusyDB(t, busy)
		ctx := t.Context()
		release := make(chan struct{})
		entered, holderDone := holdQueue(t, db, "holder", release)
		<-entered

		// Control: a write that skips the queue meets the open transaction in
		// SQLite's busy handler and fails once the busy timeout has passed.
		_, err := db.pool.ExecContext(ctx,
			`INSERT INTO runs (job, started_ts, status, stats, error, created_ts, updated_ts)
			 VALUES ('unqueued', ?, 'running', '{}', '', ?, ?)`, ts0, ts0, ts0)
		if e, ok := errors.AsType[*sqlite.Error](err); !ok || e.Code()&0xff != sqlite3.SQLITE_BUSY {
			t.Fatalf("unqueued write during the transaction error = %v, want SQLITE_BUSY", err)
		}

		done := make(chan error, 1)
		go func() {
			_, err := db.StartRun(ctx, "queued", t0)
			done <- err
		}()
		// The busy timeout runs on the real clock inside SQLite, so this
		// waits real time: three timeouts with no result means StartRun is
		// waiting in the queue, not in the busy handler.
		select {
		case err := <-done:
			t.Fatalf("StartRun() returned %v while the transaction was open, want it to wait", err)
		case <-time.After(3 * busy):
		}
		close(release)
		if err := <-holderDone; err != nil {
			t.Fatalf("holding InTx error = %v", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("queued StartRun() error = %v, want it to succeed after the commit", err)
		}
		for job, want := range map[string]int{"holder": 1, "queued": 1, "unqueued": 0} {
			if got := count(t, db, "SELECT COUNT(*) FROM runs WHERE job = ?", job); got != want {
				t.Errorf("runs for %s = %d, want %d", job, got, want)
			}
		}
	})

	t.Run("context ends while waiting", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			db := newTestDB(t)
			release := make(chan struct{})
			entered, holderDone := holdQueue(t, db, "holder", release)
			<-entered

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_, err := db.StartRun(ctx, "late", t0)
			checkQueueWaitErr(t, err, context.DeadlineExceeded)

			close(release)
			if err := <-holderDone; err != nil {
				t.Fatalf("holding InTx error = %v", err)
			}
			if got := count(t, db, "SELECT COUNT(*) FROM runs WHERE job = 'late'"); got != 0 {
				t.Errorf("runs for the timed-out StartRun = %d, want 0", got)
			}
		})
	})
}

// writeCalls calls every write method of db once with ctx. The arguments
// name rows that may not exist: the callers only check how each call waits.
func writeCalls(ctx context.Context, db *DB) map[string]func() error {
	return map[string]func() error{
		"StartRun": func() error { _, err := db.StartRun(ctx, "w", t0); return err },
		"FinishRun": func() error {
			return db.FinishRun(ctx, model.Run{ID: 1, Status: model.RunSucceeded, FinishedAt: t0})
		},
		"UpsertRepo":  func() error { _, err := db.UpsertRepo(ctx, "example-server", ""); return err },
		"RecordFetch": func() error { return db.RecordFetch(ctx, model.FetchResult{RepoID: 1, At: t0}) },
		"SaveHTTPValidator": func() error {
			return db.SaveHTTPValidator(ctx, model.HTTPValidator{URL: "https://api.example.com/x", FetchedAt: t0, Status: 200})
		},
		"UpsertFinding": func() error {
			_, err := db.UpsertFinding(ctx, model.Finding{Kind: model.KindJobFailure, Fingerprint: "job_failure|w", LastChangedAt: t0})
			return err
		},
		"ResolveFinding": func() error { return db.ResolveFinding(ctx, "job_failure|w", t0) },
		"SaveFindingState": func() error {
			return db.SaveFindingState(ctx, 1, model.FindingState{State: model.StateAcked, ChangedAt: t0})
		},
		"MarkReported": func() error { return db.MarkReported(ctx, []int64{1}, t0) },
		"SaveDigest": func() error {
			_, err := db.SaveDigest(ctx, model.DigestRecord{Channel: "stdout", Status: model.DigestSent, SentAt: t0})
			return err
		},
		"MarkDigestRetried": func() error { return db.MarkDigestRetried(ctx, 1) },
	}
}

func TestWriteQueue_EveryWriteWaits(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	release := make(chan struct{})
	entered, holderDone := holdQueue(t, db, "holder", release)
	<-entered

	// With the queue held, the cancelled context is the only ready case, so
	// a queued write gives up at once; one that skipped the queue would fail
	// in SQLite with an error that does not mention the queue.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for name, call := range writeCalls(ctx, db) {
		t.Run(name, func(t *testing.T) {
			checkQueueWaitErr(t, call(), context.Canceled)
		})
	}

	close(release)
	if err := <-holderDone; err != nil {
		t.Fatalf("holding InTx error = %v", err)
	}
	for _, table := range []string{"repos", "http_cache", "findings", "digests"} {
		if got := count(t, db, "SELECT COUNT(*) FROM "+table); got != 0 {
			t.Errorf("%s rows = %d, want 0", table, got)
		}
	}
	if got := count(t, db, "SELECT COUNT(*) FROM runs WHERE job <> 'holder'"); got != 0 {
		t.Errorf("runs besides the holder = %d, want 0", got)
	}
}

func TestWriteQueue_WritesInsideTxDoNotWaitAgain(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := newTestDB(t)
		// A write that waited for the queue its own transaction holds would
		// block until this deadline; in the bubble that is reached at once.
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		err := db.InTx(ctx, func(tx *DB) error {
			calls := writeCalls(ctx, tx)
			// Order matters: the run, repo, finding and digest must exist
			// before the methods that report an unknown ID.
			for _, name := range []string{"StartRun", "FinishRun", "UpsertRepo", "RecordFetch", "SaveHTTPValidator",
				"UpsertFinding", "SaveFindingState", "MarkReported", "ResolveFinding", "SaveDigest", "MarkDigestRetried"} {
				if err := calls[name](); err != nil {
					return errors.Join(errors.New(name), err)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("InTx() with every write inside error = %v", err)
		}
		for _, table := range []string{"runs", "repos", "http_cache", "findings", "digests"} {
			if got := count(t, db, "SELECT COUNT(*) FROM "+table); got != 1 {
				t.Errorf("%s rows after commit = %d, want 1", table, got)
			}
		}
	})
}
