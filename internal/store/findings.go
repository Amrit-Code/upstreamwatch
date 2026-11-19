package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// findingColumns selects a finding with its state. Queries using it read
// "FROM findings f JOIN finding_state s ON s.finding_id = f.id".
const findingColumns = `f.id, f.kind, f.repo_id, f.package_id, f.dep_version_id, f.advisory_id,
	f.issue_id, f.verdict_id, f.fingerprint, f.score, f.rank_reason, f.first_seen_ts,
	f.last_changed_ts, f.resolved_ts, f.reported_in_digest_ts,
	s.state, s.snooze_until_ts, s.note, s.changed_ts, s.changed_by`

// scanFinding reads one row selected with findingColumns.
func scanFinding(s scanner) (model.Finding, error) {
	var (
		f                                          model.Finding
		kind, state                                string
		repoID, packageID, depVersionID            sql.NullInt64
		issueID, verdictID                         sql.NullInt64
		advisoryID, note, changedBy                sql.NullString
		firstSeen, lastChanged, resolved, reported sql.NullString
		snoozeUntil, changedAt                     sql.NullString
	)
	if err := s.Scan(&f.ID, &kind, &repoID, &packageID, &depVersionID, &advisoryID,
		&issueID, &verdictID, &f.Fingerprint, &f.Score, &f.RankReason, &firstSeen,
		&lastChanged, &resolved, &reported,
		&state, &snoozeUntil, &note, &changedAt, &changedBy); err != nil {
		return model.Finding{}, err
	}
	f.Kind = model.FindingKind(kind)
	f.RepoID = repoID.Int64
	f.PackageID = packageID.Int64
	f.DepVersionID = depVersionID.Int64
	f.AdvisoryID = advisoryID.String
	f.IssueID = issueID.Int64
	f.VerdictID = verdictID.Int64
	f.State.State = model.FindingStateValue(state)
	f.State.Note = note.String
	f.State.ChangedBy = changedBy.String

	times := []struct {
		col string
		src sql.NullString
		dst *time.Time
	}{
		{"first_seen_ts", firstSeen, &f.FirstSeenAt},
		{"last_changed_ts", lastChanged, &f.LastChangedAt},
		{"resolved_ts", resolved, &f.ResolvedAt},
		{"reported_in_digest_ts", reported, &f.ReportedInDigestAt},
		{"snooze_until_ts", snoozeUntil, &f.State.SnoozeUntil},
		{"changed_ts", changedAt, &f.State.ChangedAt},
	}
	for _, t := range times {
		v, err := parseTime(t.src)
		if err != nil {
			return model.Finding{}, fmt.Errorf("finding %d %s: %w", f.ID, t.col, err)
		}
		*t.dst = v
	}
	return f, nil
}

// UpsertFinding inserts f, keyed by f.Fingerprint, with first-seen and
// last-changed times taken from f.LastChangedAt and a finding_state row in
// state open. If the fingerprint exists it updates the dep_version link (so a
// finding whose interval was deleted is relinked), score and rank reason, and
// clears the resolved time. The last-changed time moves to f.LastChangedAt
// only when the stored score or rank reason differs from f's or the finding
// was resolved; otherwise it is kept, so a finding a job sees again unchanged
// does not count as changed. The other links, the first-seen time, the digest
// time and the state row are left alone. Zero ID fields are stored as NULL.
// It returns the stored finding with its state.
func (db *DB) UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error) {
	var stored model.Finding
	err := db.InTx(ctx, func(tx *DB) error {
		now := formatTime(tx.now())
		changed := formatTime(f.LastChangedAt)
		var id int64
		err := tx.q.QueryRowContext(ctx,
			`INSERT INTO findings (kind, repo_id, package_id, dep_version_id, advisory_id, issue_id,
				verdict_id, fingerprint, score, rank_reason, first_seen_ts, last_changed_ts,
				created_ts, updated_ts)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (fingerprint) DO UPDATE SET
				dep_version_id = excluded.dep_version_id,
				score = excluded.score,
				rank_reason = excluded.rank_reason,
				-- Every expression on the right reads the row as it was before
				-- this update, so the comparison sees the stored values.
				last_changed_ts = CASE
					WHEN findings.score <> excluded.score
						OR findings.rank_reason <> excluded.rank_reason
						OR findings.resolved_ts IS NOT NULL
					THEN excluded.last_changed_ts
					ELSE findings.last_changed_ts
				END,
				resolved_ts = NULL,
				updated_ts = excluded.updated_ts
			 RETURNING id`,
			string(f.Kind), nullID(f.RepoID), nullID(f.PackageID), nullID(f.DepVersionID),
			nullText(f.AdvisoryID), nullID(f.IssueID), nullID(f.VerdictID), f.Fingerprint,
			f.Score, f.RankReason, changed, changed, now, now).Scan(&id)
		if err != nil {
			return fmt.Errorf("write finding: %w", err)
		}
		// An existing finding already has its state row; only a new one gets
		// the initial open state.
		if _, err := tx.q.ExecContext(ctx,
			`INSERT INTO finding_state (finding_id, state, created_ts, updated_ts)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT (finding_id) DO NOTHING`,
			id, string(model.StateOpen), now, now); err != nil {
			return fmt.Errorf("write finding state: %w", err)
		}
		stored, err = scanFinding(tx.q.QueryRowContext(ctx,
			`SELECT `+findingColumns+`
			 FROM findings f JOIN finding_state s ON s.finding_id = f.id
			 WHERE f.id = ?`, id))
		if err != nil {
			return fmt.Errorf("read finding back: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.Finding{}, fmt.Errorf("upsert finding %q: %w", f.Fingerprint, err)
	}
	return stored, nil
}

// ResolveFinding marks the unresolved finding with this fingerprint as
// resolved at the given time, which also becomes its last-changed time. It is
// not an error if there is none.
func (db *DB) ResolveFinding(ctx context.Context, fingerprint string, at time.Time) error {
	ts := formatTime(at)
	err := db.write(ctx, func() error {
		_, err := db.q.ExecContext(ctx,
			`UPDATE findings SET resolved_ts = ?, last_changed_ts = ?, updated_ts = ?
			 WHERE fingerprint = ? AND resolved_ts IS NULL`,
			ts, ts, formatTime(db.now()), fingerprint)
		return err
	})
	if err != nil {
		return fmt.Errorf("resolve finding %q: %w", fingerprint, err)
	}
	return nil
}

// queryFindings runs a SELECT of findingColumns and scans every row. It
// returns nil when there are none.
func (db *DB) queryFindings(ctx context.Context, query string, args ...any) ([]model.Finding, error) {
	rows, err := db.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var findings []model.Finding
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		findings = append(findings, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return findings, nil
}

// ListOpenFindings returns unresolved findings whose state is open or acked,
// highest score first, then lowest ID. It returns nil when there are none.
func (db *DB) ListOpenFindings(ctx context.Context) ([]model.Finding, error) {
	findings, err := db.queryFindings(ctx,
		`SELECT `+findingColumns+`
		 FROM findings f JOIN finding_state s ON s.finding_id = f.id
		 WHERE f.resolved_ts IS NULL AND s.state IN (?, ?)
		 ORDER BY f.score DESC, f.id ASC`,
		string(model.StateOpen), string(model.StateAcked))
	if err != nil {
		return nil, fmt.Errorf("list open findings: %w", err)
	}
	return findings, nil
}

// ListFindings returns the unresolved findings that pass flt, highest score
// first, then lowest ID, each with its stored state. The filter uses the
// effective state: a snoozed finding whose snooze ends at or before flt.Now
// counts as open. Without flt.Suppressed only findings whose effective state
// is open or acked are returned; with it every unresolved finding is. A
// non-zero flt.RepoID or a non-empty flt.Kind narrows the list. A zero
// flt.Now is an error. It returns nil when nothing passes.
func (db *DB) ListFindings(ctx context.Context, flt model.FindingFilter) ([]model.Finding, error) {
	if flt.Now.IsZero() {
		return nil, errors.New("list findings: filter has no current time")
	}
	suppressed := 0
	if flt.Suppressed {
		suppressed = 1
	}
	// A NULL snooze end is the zero time, which is before any flt.Now.
	findings, err := db.queryFindings(ctx,
		`SELECT `+findingColumns+`
		 FROM findings f JOIN finding_state s ON s.finding_id = f.id
		 WHERE f.resolved_ts IS NULL
		   AND (? = 1 OR s.state IN (?, ?)
		        OR (s.state = ? AND (s.snooze_until_ts IS NULL OR s.snooze_until_ts <= ?)))
		   AND (? = 0 OR f.repo_id = ?)
		   AND (? = '' OR f.kind = ?)
		 ORDER BY f.score DESC, f.id ASC`,
		suppressed, string(model.StateOpen), string(model.StateAcked),
		string(model.StateSnoozed), formatTime(flt.Now),
		flt.RepoID, flt.RepoID, string(flt.Kind), string(flt.Kind))
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	return findings, nil
}

// ListUnresolvedFindings returns every unresolved finding of kind, in any
// operator state, ordered by ID. It returns nil when there are none.
func (db *DB) ListUnresolvedFindings(ctx context.Context, kind model.FindingKind) ([]model.Finding, error) {
	findings, err := db.queryFindings(ctx,
		`SELECT `+findingColumns+`
		 FROM findings f JOIN finding_state s ON s.finding_id = f.id
		 WHERE f.resolved_ts IS NULL AND f.kind = ?
		 ORDER BY f.id ASC`,
		string(kind))
	if err != nil {
		return nil, fmt.Errorf("list unresolved %s findings: %w", kind, err)
	}
	return findings, nil
}

// GetFinding returns the finding with this ID and its state, or an error
// wrapping model.ErrNotFound.
func (db *DB) GetFinding(ctx context.Context, id int64) (model.Finding, error) {
	f, err := scanFinding(db.q.QueryRowContext(ctx,
		`SELECT `+findingColumns+`
		 FROM findings f JOIN finding_state s ON s.finding_id = f.id
		 WHERE f.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Finding{}, fmt.Errorf("finding %d: %w", id, model.ErrNotFound)
	}
	if err != nil {
		return model.Finding{}, fmt.Errorf("get finding %d: %w", id, err)
	}
	return f, nil
}

// SaveFindingState replaces the operator state of the finding with this ID
// with st: state, snooze end (NULL when zero), note (NULL when empty), change
// time (NULL when zero) and who changed it (NULL when empty). It does not
// check that the move is allowed; model.Transition does. An unknown ID is an
// error wrapping model.ErrNotFound.
func (db *DB) SaveFindingState(ctx context.Context, id int64, st model.FindingState) error {
	var n int64
	err := db.write(ctx, func() error {
		res, err := db.q.ExecContext(ctx,
			`UPDATE finding_state SET state = ?, snooze_until_ts = ?, note = ?,
				changed_ts = ?, changed_by = ?, updated_ts = ?
			 WHERE finding_id = ?`,
			string(st.State), formatTime(st.SnoozeUntil), nullText(st.Note),
			formatTime(st.ChangedAt), nullText(st.ChangedBy), formatTime(db.now()), id)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return fmt.Errorf("save state of finding %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("save state of finding %d: %w", id, model.ErrNotFound)
	}
	return nil
}

// markReportedChunk is the most IDs bound in one UPDATE ... IN (...).
const markReportedChunk = 500

// MarkReported sets the digest time of the findings with these IDs to at,
// leaving their last-changed time alone. IDs with no finding are skipped. An
// empty ids is a no-op. One transaction, however many IDs.
func (db *DB) MarkReported(ctx context.Context, ids []int64, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	err := db.InTx(ctx, func(tx *DB) error {
		now := formatTime(tx.now())
		reported := formatTime(at)
		for chunk := range slices.Chunk(ids, markReportedChunk) {
			args := make([]any, 0, len(chunk)+2)
			args = append(args, reported, now)
			for _, id := range chunk {
				args = append(args, id)
			}
			placeholders := strings.Repeat("?, ", len(chunk)-1) + "?"
			if _, err := tx.q.ExecContext(ctx,
				`UPDATE findings SET reported_in_digest_ts = ?, updated_ts = ?
				 WHERE id IN (`+placeholders+`)`, args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("mark %d findings reported: %w", len(ids), err)
	}
	return nil
}
