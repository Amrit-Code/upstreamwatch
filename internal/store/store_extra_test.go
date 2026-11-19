package store

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Gap tests for WO-04: indexes and UNIQUE constraints of requirement 4, the
// created_ts/updated_ts rule, store-clock timestamps on runs, StartRun's
// returned time, and concurrent access (store-tests template).

// indexes returns each index on table as its column list, keyed by index
// name, with whether it is unique.
func indexes(t *testing.T, db *DB, table string) map[string]struct {
	cols   []string
	unique bool
} {
	t.Helper()
	rows, err := db.pool.QueryContext(t.Context(), `SELECT name, "unique" FROM pragma_index_list(?)`, table)
	if err != nil {
		t.Fatalf("index list of %s: %v", table, err)
	}
	type idx struct {
		name   string
		unique bool
	}
	var list []idx
	for rows.Next() {
		var i idx
		if err := rows.Scan(&i.name, &i.unique); err != nil {
			t.Fatalf("scan index list: %v", err)
		}
		list = append(list, i)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index list rows: %v", err)
	}
	rows.Close()

	out := map[string]struct {
		cols   []string
		unique bool
	}{}
	for _, i := range list {
		crows, err := db.pool.QueryContext(t.Context(), `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, i.name)
		if err != nil {
			t.Fatalf("index info of %s: %v", i.name, err)
		}
		var cols []string
		for crows.Next() {
			var c string
			if err := crows.Scan(&c); err != nil {
				t.Fatalf("scan index info: %v", err)
			}
			cols = append(cols, c)
		}
		if err := crows.Err(); err != nil {
			t.Fatalf("index info rows: %v", err)
		}
		crows.Close()
		out[i.name] = struct {
			cols   []string
			unique bool
		}{cols, i.unique}
	}
	return out
}

// tables returns every table name in the schema except SQLite's own.
func tables(t *testing.T, db *DB) []string {
	t.Helper()
	rows, err := db.pool.QueryContext(t.Context(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table rows: %v", err)
	}
	return names
}

func TestSchema_QueryIndexes(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	for _, tc := range []struct {
		table string
		cols  []string
	}{
		{"runs", []string{"job", "id"}},       // LatestRuns
		{"findings", []string{"resolved_ts"}}, // ListOpenFindings
	} {
		found := false
		for _, idx := range indexes(t, db, tc.table) {
			if reflect.DeepEqual(idx.cols, tc.cols) {
				found = true
			}
		}
		if !found {
			t.Errorf("no index on %s(%s)", tc.table, strings.Join(tc.cols, ", "))
		}
	}
}

func TestSchema_DocUniqueConstraints(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	// Every UNIQUE stated in docs/database.md, "Tables and columns".
	for _, tc := range []struct {
		table string
		cols  []string
	}{
		{"repos", []string{"slug"}},
		{"commits", []string{"repo_id", "sha"}},
		{"manifests", []string{"repo_id", "path"}},
		{"packages", []string{"purl"}},
		{"dep_events", []string{"commit_id", "manifest_id", "package_id"}},
		{"upstream_repos", []string{"url"}},
		{"advisory_matches", []string{"dep_version_id", "advisory_id"}},
		{"upstream_issues", []string{"upstream_repo_id", "number"}},
		{"upstream_issue_comments", []string{"issue_id", "external_id"}},
		{"issue_candidates", []string{"issue_id", "repo_id", "package_id", "issue_body_hash"}},
		{"llm_verdicts", []string{"cache_key"}},
		{"findings", []string{"fingerprint"}},
		{"eval_labels", []string{"issue_id", "repo_id", "package_id", "pinned_version"}},
	} {
		found := false
		for _, idx := range indexes(t, db, tc.table) {
			if idx.unique && reflect.DeepEqual(idx.cols, tc.cols) {
				found = true
			}
		}
		if !found {
			t.Errorf("no UNIQUE constraint on %s(%s)", tc.table, strings.Join(tc.cols, ", "))
		}
	}
}

func TestSchema_ForeignKeyChildColumnsIndexed(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	checked := 0
	for _, table := range tables(t, db) {
		rows, err := db.pool.QueryContext(t.Context(), `SELECT "from" FROM pragma_foreign_key_list(?)`, table)
		if err != nil {
			t.Fatalf("foreign keys of %s: %v", table, err)
		}
		var fromCols []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatalf("scan foreign key: %v", err)
			}
			fromCols = append(fromCols, c)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("foreign key rows: %v", err)
		}
		rows.Close()
		if len(fromCols) == 0 {
			continue
		}
		idx := indexes(t, db, table)
		for _, col := range fromCols {
			checked++
			// An INTEGER PRIMARY KEY is the rowid and needs no index.
			if count(t, db, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ? AND pk = 1 AND upper(type) = 'INTEGER'`, table, col) == 1 &&
				count(t, db, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE pk > 0`, table) == 1 {
				continue
			}
			covered := false
			for _, i := range idx {
				if len(i.cols) > 0 && i.cols[0] == col {
					covered = true
				}
			}
			if !covered {
				t.Errorf("foreign-key column %s.%s has no index leading with it", table, col)
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no foreign keys; the schema query is wrong")
	}
}

func TestSchema_TimestampColumns(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	// These keep the doc's columns plus updated_ts, and no created_ts.
	updatedOnly := map[string]bool{"kv": true, "http_cache": true, "settings": true}
	for _, table := range tables(t, db) {
		if table == "schema_migrations" {
			continue
		}
		notNull := func(col string) int {
			return count(t, db, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ? AND "notnull" = 1 AND upper(type) = 'TEXT'`, table, col)
		}
		present := func(col string) int {
			return count(t, db, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, col)
		}
		if notNull("updated_ts") != 1 {
			t.Errorf("%s.updated_ts missing or not TEXT NOT NULL", table)
		}
		if updatedOnly[table] {
			if present("created_ts") != 0 {
				t.Errorf("%s has created_ts, want only updated_ts", table)
			}
			continue
		}
		if notNull("created_ts") != 1 {
			t.Errorf("%s.created_ts missing or not TEXT NOT NULL", table)
		}
	}
}

// rowTimes reads created_ts and updated_ts of one row.
func rowTimes(t *testing.T, db *DB, table, keyCol string, key int64) (created, updated string) {
	t.Helper()
	q := fmt.Sprintf("SELECT created_ts, updated_ts FROM %s WHERE %s = ?", table, keyCol)
	if err := db.pool.QueryRowContext(t.Context(), q, key).Scan(&created, &updated); err != nil {
		t.Fatalf("read %s timestamps: %v", table, err)
	}
	return created, updated
}

func TestDB_RowTimestampsFromStoreClock(t *testing.T) {
	t.Parallel()

	t.Run("runs", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		// The run's own times differ from the store clock, to show which is used.
		r, err := db.StartRun(t.Context(), "ingest", t0.Add(-24*time.Hour))
		if err != nil {
			t.Fatalf("StartRun() error = %v", err)
		}
		if c, u := rowTimes(t, db, "runs", "id", r.ID); c != ts0 || u != ts0 {
			t.Errorf("after StartRun created_ts, updated_ts = %s, %s; want %s, %s", c, u, ts0, ts0)
		}
		db.now = func() time.Time { return t0.Add(time.Hour) }
		r.Status, r.FinishedAt = model.RunSucceeded, t0.Add(-23*time.Hour)
		if err := db.FinishRun(t.Context(), r); err != nil {
			t.Fatalf("FinishRun() error = %v", err)
		}
		if c, u := rowTimes(t, db, "runs", "id", r.ID); c != ts0 || u != "2026-03-01T13:00:00Z" {
			t.Errorf("after FinishRun created_ts, updated_ts = %s, %s; want %s, 2026-03-01T13:00:00Z", c, u, ts0)
		}
	})

	t.Run("findings", func(t *testing.T) {
		t.Parallel()
		db := newTestDB(t)
		in := model.Finding{Kind: model.KindJobFailure, Fingerprint: "job_failure|ingest", LastChangedAt: t0.Add(-24 * time.Hour)}
		f, err := db.UpsertFinding(t.Context(), in)
		if err != nil {
			t.Fatalf("UpsertFinding() error = %v", err)
		}
		db.now = func() time.Time { return t0.Add(time.Hour) }
		in.LastChangedAt = t0.Add(-23 * time.Hour)
		if _, err := db.UpsertFinding(t.Context(), in); err != nil {
			t.Fatalf("second UpsertFinding() error = %v", err)
		}
		if c, u := rowTimes(t, db, "findings", "id", f.ID); c != ts0 || u != "2026-03-01T13:00:00Z" {
			t.Errorf("after re-upsert findings created_ts, updated_ts = %s, %s; want %s, 2026-03-01T13:00:00Z", c, u, ts0)
		}
		if c, u := rowTimes(t, db, "finding_state", "finding_id", f.ID); c != ts0 || u != ts0 {
			t.Errorf("after re-upsert finding_state created_ts, updated_ts = %s, %s; want both %s (state row untouched)", c, u, ts0)
		}
		db.now = func() time.Time { return t0.Add(2 * time.Hour) }
		if err := db.ResolveFinding(t.Context(), in.Fingerprint, t0.Add(-22*time.Hour)); err != nil {
			t.Fatalf("ResolveFinding() error = %v", err)
		}
		if c, u := rowTimes(t, db, "findings", "id", f.ID); c != ts0 || u != "2026-03-01T14:00:00Z" {
			t.Errorf("after resolve findings created_ts, updated_ts = %s, %s; want %s, 2026-03-01T14:00:00Z", c, u, ts0)
		}
	})
}

func TestDB_StartRun_ReturnsStoredTime(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	zone := time.FixedZone("UTC-7", -7*3600)
	in := time.Date(2026, 3, 1, 5, 0, 5, 999999999, zone)
	got, err := db.StartRun(t.Context(), "ingest", in)
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	want := time.Date(2026, 3, 1, 12, 0, 5, 0, time.UTC)
	if !got.StartedAt.Equal(want) || got.StartedAt.Location() != time.UTC {
		t.Errorf("StartRun() StartedAt = %v, want %v in UTC", got.StartedAt, want)
	}
	runs, err := db.LatestRuns(t.Context())
	if err != nil || len(runs) != 1 || !reflect.DeepEqual(runs[0], got) {
		t.Errorf("LatestRuns() = %+v, %v; want [%+v], the run StartRun returned", runs, err, got)
	}
}

func TestDB_ConcurrentWriters(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	const writers, each = 8, 20
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := range writers {
		wg.Go(func() {
			for i := range each {
				err := db.InTx(t.Context(), func(tx *DB) error {
					r, err := tx.StartRun(t.Context(), fmt.Sprintf("job-%d", w), t0)
					if err != nil {
						return err
					}
					r.Status, r.FinishedAt = model.RunSucceeded, t0
					if err := tx.FinishRun(t.Context(), r); err != nil {
						return err
					}
					_, err = tx.UpsertFinding(t.Context(), model.Finding{
						Kind: model.KindJobFailure, Fingerprint: fmt.Sprintf("fp-%d-%d", w, i), LastChangedAt: t0,
					})
					return err
				})
				if err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent InTx error = %v, want every writer to wait and succeed", err)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM runs WHERE status = 'succeeded'"); got != writers*each {
		t.Errorf("succeeded runs = %d, want %d", got, writers*each)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM finding_state"); got != writers*each {
		t.Errorf("finding_state rows = %d, want %d", got, writers*each)
	}
}

func TestDB_ReadDuringWriteTransaction(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if _, err := db.StartRun(t.Context(), "ingest", t0); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	errInner := errors.New("checked")
	err := db.InTx(t.Context(), func(tx *DB) error {
		if _, err := tx.StartRun(t.Context(), "timeline", t0); err != nil {
			return err
		}
		// A reader outside the transaction is not blocked and does not see
		// the uncommitted row.
		runs, err := db.LatestRuns(t.Context())
		if err != nil {
			t.Errorf("LatestRuns() during a write transaction error = %v", err)
		}
		if len(runs) != 1 || runs[0].Job != "ingest" {
			t.Errorf("LatestRuns() during a write transaction = %+v, want only the committed ingest run", runs)
		}
		return errInner
	})
	if !errors.Is(err, errInner) {
		t.Fatalf("InTx() error = %v, want %v", err, errInner)
	}
}
