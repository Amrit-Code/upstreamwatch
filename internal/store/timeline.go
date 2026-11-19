package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// encodeDirect maps a Directness to the nullable direct column: unknown is
// NULL, yes is 1 and no is 0.
func encodeDirect(d model.Directness) (any, error) {
	switch d {
	case model.DirectUnknown:
		return nil, nil
	case model.DirectYes:
		return int64(1), nil
	case model.DirectNo:
		return int64(0), nil
	}
	return nil, fmt.Errorf("invalid directness %d", d)
}

// decodeDirect maps a stored direct column back to a Directness.
func decodeDirect(v sql.NullInt64) (model.Directness, error) {
	if !v.Valid {
		return model.DirectUnknown, nil
	}
	switch v.Int64 {
	case 1:
		return model.DirectYes, nil
	case 0:
		return model.DirectNo, nil
	}
	return model.DirectUnknown, fmt.Errorf("invalid stored direct value %d", v.Int64)
}

// eventColumns selects an event with its commit, manifest and package. Queries
// using it read "FROM dep_events e JOIN commits c ON c.id = e.commit_id
// JOIN manifests m ON m.id = e.manifest_id JOIN packages p ON p.id = e.package_id".
const eventColumns = `c.sha, c.committed_ts, m.path, p.purl, p.ecosystem, p.name,
	e.change, e.version, e.requirement, e.direct, e.scope, e.prev_version`

// scanEvent reads one row selected with eventColumns.
func scanEvent(s scanner) (model.DepEvent, error) {
	var (
		e                                    model.DepEvent
		committed, ecosystem, name, change   sql.NullString
		version, requirement, scope, prevVer sql.NullString
		direct                               sql.NullInt64
	)
	if err := s.Scan(&e.CommitSHA, &committed, &e.ManifestPath, &e.Package.PURL, &ecosystem, &name,
		&change, &version, &requirement, &direct, &scope, &prevVer); err != nil {
		return model.DepEvent{}, err
	}
	e.Package.Ecosystem = ecosystem.String
	e.Package.Name = name.String
	e.Change = model.Change(change.String)
	e.Version = version.String
	e.Requirement = requirement.String
	e.Scope = scope.String
	e.PrevVersion = prevVer.String
	var err error
	if e.CommittedAt, err = parseTime(committed); err != nil {
		return model.DepEvent{}, fmt.Errorf("event at commit %s committed_ts: %w", e.CommitSHA, err)
	}
	if e.Direct, err = decodeDirect(direct); err != nil {
		return model.DepEvent{}, fmt.Errorf("event at commit %s: %w", e.CommitSHA, err)
	}
	return e, nil
}

// intervalColumns selects an interval with its manifest, package and commits.
// Queries using it read "FROM dep_versions v JOIN manifests m ON m.id =
// v.manifest_id JOIN packages p ON p.id = v.package_id LEFT JOIN commits fc ON
// fc.id = v.from_commit_id LEFT JOIN commits tc ON tc.id = v.to_commit_id".
const intervalColumns = `v.id, m.path, p.purl, p.ecosystem, p.name, v.version, v.requirement,
	v.direct, fc.sha, v.from_ts, tc.sha, v.to_ts`

// scanInterval reads one row selected with intervalColumns.
func scanInterval(s scanner) (model.DepInterval, error) {
	var (
		iv                                    model.DepInterval
		ecosystem, name, version, requirement sql.NullString
		fromSHA, fromTS, toSHA, toTS          sql.NullString
		direct                                sql.NullInt64
	)
	if err := s.Scan(&iv.ID, &iv.ManifestPath, &iv.Package.PURL, &ecosystem, &name, &version,
		&requirement, &direct, &fromSHA, &fromTS, &toSHA, &toTS); err != nil {
		return model.DepInterval{}, err
	}
	iv.Package.Ecosystem = ecosystem.String
	iv.Package.Name = name.String
	iv.Version = version.String
	iv.Requirement = requirement.String
	iv.FromSHA = fromSHA.String
	iv.ToSHA = toSHA.String
	var err error
	if iv.Direct, err = decodeDirect(direct); err != nil {
		return model.DepInterval{}, fmt.Errorf("interval %d: %w", iv.ID, err)
	}
	if iv.From, err = parseTime(fromTS); err != nil {
		return model.DepInterval{}, fmt.Errorf("interval %d from_ts: %w", iv.ID, err)
	}
	if iv.To, err = parseTime(toTS); err != nil {
		return model.DepInterval{}, fmt.Errorf("interval %d to_ts: %w", iv.ID, err)
	}
	return iv, nil
}

// ListEvents returns every stored event of the repo, ordered by commit
// position, then manifest path, then package URL. It returns nil when there
// are none.
func (db *DB) ListEvents(ctx context.Context, repoID int64) ([]model.DepEvent, error) {
	rows, err := db.q.QueryContext(ctx,
		`SELECT `+eventColumns+`
		 FROM dep_events e
		 JOIN commits c ON c.id = e.commit_id
		 JOIN manifests m ON m.id = e.manifest_id
		 JOIN packages p ON p.id = e.package_id
		 WHERE e.repo_id = ?
		 ORDER BY c.position, m.path, p.purl`, repoID)
	if err != nil {
		return nil, fmt.Errorf("list events of repo %d: %w", repoID, err)
	}
	defer rows.Close()
	var events []model.DepEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("list events of repo %d: %w", repoID, err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list events of repo %d: %w", repoID, err)
	}
	return events, nil
}

// ListIntervals returns the repo's intervals with their row IDs, ordered by
// package URL, then manifest path, then opening time, then ID. It returns nil
// when there are none.
func (db *DB) ListIntervals(ctx context.Context, repoID int64) ([]model.DepInterval, error) {
	rows, err := db.q.QueryContext(ctx,
		`SELECT `+intervalColumns+`
		 FROM dep_versions v
		 JOIN manifests m ON m.id = v.manifest_id
		 JOIN packages p ON p.id = v.package_id
		 LEFT JOIN commits fc ON fc.id = v.from_commit_id
		 LEFT JOIN commits tc ON tc.id = v.to_commit_id
		 WHERE v.repo_id = ?
		 ORDER BY p.purl, m.path, v.from_ts, v.id`, repoID)
	if err != nil {
		return nil, fmt.Errorf("list intervals of repo %d: %w", repoID, err)
	}
	defer rows.Close()
	var intervals []model.DepInterval
	for rows.Next() {
		iv, err := scanInterval(rows)
		if err != nil {
			return nil, fmt.Errorf("list intervals of repo %d: %w", repoID, err)
		}
		intervals = append(intervals, iv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list intervals of repo %d: %w", repoID, err)
	}
	return intervals, nil
}

// ApplyTimeline writes u in one transaction, so on any error nothing of it is
// stored. With u.Reset it first deletes the repo's intervals, events and
// commits (manifests and packages stay). It then appends u.Commits after the
// repo's highest stored position, upserts the manifests in u.Manifests and the
// packages named by u.Events and u.Intervals, inserts u.Events, replaces the
// repo's intervals with u.Intervals keeping the row ID of every interval whose
// manifest, package and opening commit are unchanged, and sets the repo's last
// indexed commit to u.IndexedSHA. An event's commit must be in u.Commits or
// already stored for the repo, and an event's or interval's manifest path must
// be in u.Manifests or already stored. Rows whose values do not change are
// not written. An unknown repo ID is an error wrapping model.ErrNotFound.
func (db *DB) ApplyTimeline(ctx context.Context, u model.TimelineUpdate) error {
	err := db.InTx(ctx, func(tx *DB) error {
		a := &timelineApply{db: tx, repoID: u.RepoID, now: formatTime(tx.now()),
			commits: map[string]int64{}, manifests: map[string]int64{}, packages: map[string]int64{}}
		return a.apply(ctx, u)
	})
	if err != nil {
		return fmt.Errorf("apply timeline of repo %d: %w", u.RepoID, err)
	}
	return nil
}

// timelineApply holds the state of one ApplyTimeline call: the transaction and
// the row IDs resolved so far, keyed by commit SHA, manifest path and purl.
type timelineApply struct {
	db        *DB
	repoID    int64
	now       any
	commits   map[string]int64
	manifests map[string]int64
	packages  map[string]int64
}

func (a *timelineApply) apply(ctx context.Context, u model.TimelineUpdate) error {
	var indexed sql.NullString
	err := a.db.q.QueryRowContext(ctx,
		`SELECT last_indexed_sha FROM repos WHERE id = ?`, a.repoID).Scan(&indexed)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("repo %d: %w", a.repoID, model.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("read repo: %w", err)
	}
	if u.Reset {
		if err := a.reset(ctx); err != nil {
			return err
		}
	}
	if err := a.insertCommits(ctx, u.Commits); err != nil {
		return err
	}
	if err := a.upsertManifests(ctx, u.Manifests); err != nil {
		return err
	}
	if err := a.upsertPackages(ctx, u.Events, u.Intervals); err != nil {
		return err
	}
	if err := a.insertEvents(ctx, u.Events); err != nil {
		return err
	}
	if err := a.replaceIntervals(ctx, u.Intervals); err != nil {
		return err
	}
	if indexed.String != u.IndexedSHA {
		if _, err := a.db.q.ExecContext(ctx,
			`UPDATE repos SET last_indexed_sha = ?, updated_ts = ? WHERE id = ?`,
			nullText(u.IndexedSHA), a.now, a.repoID); err != nil {
			return fmt.Errorf("set last indexed commit: %w", err)
		}
	}
	return nil
}

// reset deletes the repo's intervals, events and commits, children first.
// Deleting intervals cascades to advisory_matches and issue_candidates and
// sets findings.dep_version_id to NULL.
func (a *timelineApply) reset(ctx context.Context) error {
	for _, table := range []string{"dep_versions", "dep_events", "commits"} {
		// The table name comes from the fixed list above, never from input.
		if _, err := a.db.q.ExecContext(ctx, `DELETE FROM `+table+` WHERE repo_id = ?`, a.repoID); err != nil {
			return fmt.Errorf("reset %s: %w", table, err)
		}
	}
	return nil
}

// insertCommits inserts commits in order, numbering them after the repo's
// highest stored position.
func (a *timelineApply) insertCommits(ctx context.Context, commits []model.Commit) error {
	if len(commits) == 0 {
		return nil
	}
	var next int64
	if err := a.db.q.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position), -1) + 1 FROM commits WHERE repo_id = ?`, a.repoID).Scan(&next); err != nil {
		return fmt.Errorf("read last commit position: %w", err)
	}
	for i, c := range commits {
		var id int64
		err := a.db.q.QueryRowContext(ctx,
			`INSERT INTO commits (repo_id, sha, position, committed_ts, author_name, author_email,
				subject, created_ts, updated_ts)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			a.repoID, c.SHA, next+int64(i), formatTime(c.CommittedAt), nullText(c.AuthorName),
			nullText(c.AuthorEmail), nullText(c.Subject), a.now, a.now).Scan(&id)
		if err != nil {
			return fmt.Errorf("insert commit %s: %w", c.SHA, err)
		}
		a.commits[c.SHA] = id
	}
	return nil
}

// upsertManifests upserts one manifests row per path, in path order so row
// IDs do not depend on map iteration. A row whose ecosystem and kind are
// unchanged is not rewritten.
func (a *timelineApply) upsertManifests(ctx context.Context, manifests map[string]model.ManifestInfo) error {
	paths := make([]string, 0, len(manifests))
	for p := range manifests {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		info := manifests[p]
		if _, err := a.db.q.ExecContext(ctx,
			`INSERT INTO manifests (repo_id, path, ecosystem, kind, created_ts, updated_ts)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT (repo_id, path) DO UPDATE SET
				ecosystem = excluded.ecosystem,
				kind = excluded.kind,
				updated_ts = excluded.updated_ts
			 WHERE ecosystem IS NOT excluded.ecosystem OR kind IS NOT excluded.kind`,
			a.repoID, p, nullText(info.Ecosystem), nullText(string(info.Kind)), a.now, a.now); err != nil {
			return fmt.Errorf("upsert manifest %q: %w", p, err)
		}
	}
	return nil
}

// upsertPackages upserts one packages row per distinct purl in events and
// intervals, in order of first appearance. A row whose ecosystem and name are
// unchanged is not rewritten.
func (a *timelineApply) upsertPackages(ctx context.Context, events []model.DepEvent, intervals []model.DepInterval) error {
	var pkgs []model.Package
	seen := map[string]bool{}
	add := func(p model.Package) {
		if !seen[p.PURL] {
			seen[p.PURL] = true
			pkgs = append(pkgs, p)
		}
	}
	for _, e := range events {
		add(e.Package)
	}
	for _, iv := range intervals {
		add(iv.Package)
	}
	for _, p := range pkgs {
		if _, err := a.db.q.ExecContext(ctx,
			`INSERT INTO packages (purl, ecosystem, name, created_ts, updated_ts)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT (purl) DO UPDATE SET
				ecosystem = excluded.ecosystem,
				name = excluded.name,
				updated_ts = excluded.updated_ts
			 WHERE ecosystem IS NOT excluded.ecosystem OR name IS NOT excluded.name`,
			p.PURL, nullText(p.Ecosystem), nullText(p.Name), a.now, a.now); err != nil {
			return fmt.Errorf("upsert package %q: %w", p.PURL, err)
		}
	}
	return nil
}

// commitID resolves sha to a commits row of the repo.
func (a *timelineApply) commitID(ctx context.Context, sha string) (int64, error) {
	if id, ok := a.commits[sha]; ok {
		return id, nil
	}
	var id int64
	err := a.db.q.QueryRowContext(ctx,
		`SELECT id FROM commits WHERE repo_id = ? AND sha = ?`, a.repoID, sha).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("commit %q is not stored for the repo", sha)
	}
	if err != nil {
		return 0, fmt.Errorf("look up commit %q: %w", sha, err)
	}
	a.commits[sha] = id
	return id, nil
}

// manifestID resolves path to a manifests row of the repo.
func (a *timelineApply) manifestID(ctx context.Context, path string) (int64, error) {
	if id, ok := a.manifests[path]; ok {
		return id, nil
	}
	var id int64
	err := a.db.q.QueryRowContext(ctx,
		`SELECT id FROM manifests WHERE repo_id = ? AND path = ?`, a.repoID, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("manifest %q is not stored for the repo", path)
	}
	if err != nil {
		return 0, fmt.Errorf("look up manifest %q: %w", path, err)
	}
	a.manifests[path] = id
	return id, nil
}

// packageID resolves purl to its packages row.
func (a *timelineApply) packageID(ctx context.Context, purl string) (int64, error) {
	if id, ok := a.packages[purl]; ok {
		return id, nil
	}
	var id int64
	if err := a.db.q.QueryRowContext(ctx,
		`SELECT id FROM packages WHERE purl = ?`, purl).Scan(&id); err != nil {
		return 0, fmt.Errorf("look up package %q: %w", purl, err)
	}
	a.packages[purl] = id
	return id, nil
}

// insertEvents inserts events, resolving their commit, manifest and package.
func (a *timelineApply) insertEvents(ctx context.Context, events []model.DepEvent) error {
	for _, e := range events {
		commitID, err := a.commitID(ctx, e.CommitSHA)
		if err != nil {
			return fmt.Errorf("event for %s: %w", e.Package.PURL, err)
		}
		manifestID, err := a.manifestID(ctx, e.ManifestPath)
		if err != nil {
			return fmt.Errorf("event for %s at %s: %w", e.Package.PURL, e.CommitSHA, err)
		}
		packageID, err := a.packageID(ctx, e.Package.PURL)
		if err != nil {
			return fmt.Errorf("event at %s: %w", e.CommitSHA, err)
		}
		direct, err := encodeDirect(e.Direct)
		if err != nil {
			return fmt.Errorf("event for %s at %s: %w", e.Package.PURL, e.CommitSHA, err)
		}
		if _, err := a.db.q.ExecContext(ctx,
			`INSERT INTO dep_events (repo_id, commit_id, manifest_id, package_id, change, version,
				prev_version, requirement, direct, scope, created_ts, updated_ts)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.repoID, commitID, manifestID, packageID, string(e.Change), nullText(e.Version),
			nullText(e.PrevVersion), nullText(e.Requirement), direct, nullText(e.Scope),
			a.now, a.now); err != nil {
			return fmt.Errorf("insert event for %s in %s at %s: %w", e.Package.PURL, e.ManifestPath, e.CommitSHA, err)
		}
	}
	return nil
}

// intervalKey is the identity of an interval within a repo.
type intervalKey struct {
	manifestID, packageID, fromCommitID int64
}

// intervalRow is the stored or wanted state of one dep_versions row, with
// NULLs as empty values so two rows compare with ==.
type intervalRow struct {
	id          int64
	version     string
	requirement string
	direct      model.Directness
	fromTS      string
	toCommitID  int64
	toTS        string
}

// storedIntervals reads the repo's dep_versions rows keyed by identity.
func (a *timelineApply) storedIntervals(ctx context.Context) (map[intervalKey]intervalRow, error) {
	rows, err := a.db.q.QueryContext(ctx,
		`SELECT id, manifest_id, package_id, from_commit_id, version, requirement, direct,
			from_ts, to_commit_id, to_ts
		 FROM dep_versions WHERE repo_id = ?`, a.repoID)
	if err != nil {
		return nil, fmt.Errorf("read stored intervals: %w", err)
	}
	defer rows.Close()
	stored := map[intervalKey]intervalRow{}
	for rows.Next() {
		var (
			k                                  intervalKey
			r                                  intervalRow
			fromCommit, toCommit, direct       sql.NullInt64
			version, requirement, fromTS, toTS sql.NullString
		)
		if err := rows.Scan(&r.id, &k.manifestID, &k.packageID, &fromCommit, &version, &requirement,
			&direct, &fromTS, &toCommit, &toTS); err != nil {
			return nil, fmt.Errorf("read stored intervals: %w", err)
		}
		k.fromCommitID = fromCommit.Int64
		r.version, r.requirement = version.String, requirement.String
		r.fromTS, r.toTS = fromTS.String, toTS.String
		r.toCommitID = toCommit.Int64
		if r.direct, err = decodeDirect(direct); err != nil {
			return nil, fmt.Errorf("read stored interval %d: %w", r.id, err)
		}
		stored[k] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stored intervals: %w", err)
	}
	return stored, nil
}

// resolveInterval returns the identity of iv and the row values it should be
// stored with. Its opening commit must resolve within the repo; an open
// interval (no ToSHA) stores no closing commit or time.
func (a *timelineApply) resolveInterval(ctx context.Context, iv model.DepInterval) (intervalKey, intervalRow, error) {
	var (
		k   intervalKey
		r   intervalRow
		err error
	)
	if k.manifestID, err = a.manifestID(ctx, iv.ManifestPath); err != nil {
		return k, r, err
	}
	if k.packageID, err = a.packageID(ctx, iv.Package.PURL); err != nil {
		return k, r, err
	}
	if k.fromCommitID, err = a.commitID(ctx, iv.FromSHA); err != nil {
		return k, r, fmt.Errorf("opening %w", err)
	}
	r.version, r.requirement, r.direct = iv.Version, iv.Requirement, iv.Direct
	// formatTime gives nil for the zero time, which the assertion turns into "".
	r.fromTS, _ = formatTime(iv.From).(string)
	if !iv.IsOpen() {
		if iv.To.IsZero() {
			return k, r, errors.New("closed interval has no closing time")
		}
		if r.toCommitID, err = a.commitID(ctx, iv.ToSHA); err != nil {
			return k, r, fmt.Errorf("closing %w", err)
		}
		r.toTS, _ = formatTime(iv.To).(string)
	}
	return k, r, nil
}

// replaceIntervals makes the repo's dep_versions rows equal to intervals:
// rows whose identity is in the new set keep their ID and are updated if a
// value changed, rows whose identity is absent are deleted, and new
// identities are inserted.
func (a *timelineApply) replaceIntervals(ctx context.Context, intervals []model.DepInterval) error {
	stored, err := a.storedIntervals(ctx)
	if err != nil {
		return err
	}
	keys := make([]intervalKey, len(intervals))
	wanted := make([]intervalRow, len(intervals))
	inNew := make(map[intervalKey]bool, len(intervals))
	for i, iv := range intervals {
		k, r, err := a.resolveInterval(ctx, iv)
		if err != nil {
			return fmt.Errorf("interval of %s in %s from %s: %w", iv.Package.PURL, iv.ManifestPath, iv.FromSHA, err)
		}
		if inNew[k] {
			return fmt.Errorf("interval of %s in %s from %s appears twice", iv.Package.PURL, iv.ManifestPath, iv.FromSHA)
		}
		inNew[k] = true
		keys[i], wanted[i] = k, r
	}

	var vanished []int64
	for k, r := range stored {
		if !inNew[k] {
			vanished = append(vanished, r.id)
		}
	}
	slices.Sort(vanished)
	for _, id := range vanished {
		if _, err := a.db.q.ExecContext(ctx, `DELETE FROM dep_versions WHERE id = ?`, id); err != nil {
			return fmt.Errorf("delete interval %d: %w", id, err)
		}
	}

	for i, k := range keys {
		w := wanted[i]
		direct, err := encodeDirect(w.direct)
		if err != nil {
			return fmt.Errorf("interval of %s in %s: %w", intervals[i].Package.PURL, intervals[i].ManifestPath, err)
		}
		old, ok := stored[k]
		w.id = old.id
		switch {
		case !ok:
			if _, err := a.db.q.ExecContext(ctx,
				`INSERT INTO dep_versions (repo_id, package_id, manifest_id, version, requirement, direct,
					from_commit_id, from_ts, to_commit_id, to_ts, created_ts, updated_ts)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				a.repoID, k.packageID, k.manifestID, nullText(w.version), nullText(w.requirement), direct,
				k.fromCommitID, nullText(w.fromTS), nullID(w.toCommitID), nullText(w.toTS),
				a.now, a.now); err != nil {
				return fmt.Errorf("insert interval of %s in %s: %w", intervals[i].Package.PURL, intervals[i].ManifestPath, err)
			}
		case w != old:
			if _, err := a.db.q.ExecContext(ctx,
				`UPDATE dep_versions SET version = ?, requirement = ?, direct = ?, from_ts = ?,
					to_commit_id = ?, to_ts = ?, updated_ts = ?
				 WHERE id = ?`,
				nullText(w.version), nullText(w.requirement), direct, nullText(w.fromTS),
				nullID(w.toCommitID), nullText(w.toTS), a.now, old.id); err != nil {
				return fmt.Errorf("update interval %d: %w", old.id, err)
			}
		}
	}
	return nil
}
