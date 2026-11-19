package store

import (
	"context"
	"database/sql"
	"fmt"
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
// state open. If the fingerprint exists it updates score, rank reason and
// last-changed time, clears the resolved time, and leaves the first-seen time,
// the digest time and the state row alone. Zero ID fields are stored as NULL.
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
				score = excluded.score,
				rank_reason = excluded.rank_reason,
				last_changed_ts = excluded.last_changed_ts,
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
	if _, err := db.q.ExecContext(ctx,
		`UPDATE findings SET resolved_ts = ?, last_changed_ts = ?, updated_ts = ?
		 WHERE fingerprint = ? AND resolved_ts IS NULL`,
		ts, ts, formatTime(db.now()), fingerprint); err != nil {
		return fmt.Errorf("resolve finding %q: %w", fingerprint, err)
	}
	return nil
}

// ListOpenFindings returns unresolved findings whose state is open or acked,
// highest score first, then lowest ID. It returns nil when there are none.
func (db *DB) ListOpenFindings(ctx context.Context) ([]model.Finding, error) {
	rows, err := db.q.QueryContext(ctx,
		`SELECT `+findingColumns+`
		 FROM findings f JOIN finding_state s ON s.finding_id = f.id
		 WHERE f.resolved_ts IS NULL AND s.state IN (?, ?)
		 ORDER BY f.score DESC, f.id ASC`,
		string(model.StateOpen), string(model.StateAcked))
	if err != nil {
		return nil, fmt.Errorf("list open findings: %w", err)
	}
	defer rows.Close()
	var findings []model.Finding
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, fmt.Errorf("list open findings: %w", err)
		}
		findings = append(findings, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list open findings: %w", err)
	}
	return findings, nil
}
