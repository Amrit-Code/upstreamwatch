package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// advChunk is the most values bound in one IN (...) list, well under SQLite's
// variable limit.
const advChunk = 500

// advRead runs fn with a *DB whose reads agree with each other: inside a
// caller's transaction it uses that transaction, otherwise it opens a
// read-only transaction, which the driver starts with a plain BEGIN so it
// never takes the write lock.
func (db *DB) advRead(ctx context.Context, fn func(r *DB) error) error {
	if db.inTx {
		return fn(db)
	}
	sqlTx, err := db.pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read transaction: %w", err)
	}
	// Nothing was written, so the rollback only ends the read snapshot.
	defer func() { _ = sqlTx.Rollback() }()
	r := *db
	r.q = sqlTx
	return fn(&r)
}

// advPrefixScanner scans extra leading columns into prefix before handing the
// remaining columns to the destinations of a shared scan helper.
type advPrefixScanner struct {
	s      scanner
	prefix []any
}

func (p advPrefixScanner) Scan(dest ...any) error {
	return p.s.Scan(append(slices.Clip(p.prefix), dest...)...)
}

// advRangeJSON is one OSV range as stored in advisory_affected.ranges.
type advRangeJSON struct {
	Type   string         `json:"type"`
	Repo   string         `json:"repo,omitempty"`
	Events []advEventJSON `json:"events"`
}

// advEventJSON is one OSV range event; exactly one field is non-empty, so the
// stored object has exactly one key.
type advEventJSON struct {
	Introduced   string `json:"introduced,omitempty"`
	Fixed        string `json:"fixed,omitempty"`
	LastAffected string `json:"last_affected,omitempty"`
	Limit        string `json:"limit,omitempty"`
}

// advEncodeRanges renders ranges as the OSV ranges[] JSON array ("[]" when
// there are none). Every event must set exactly one field.
func advEncodeRanges(ranges []model.AffectedRange) (string, error) {
	out := make([]advRangeJSON, 0, len(ranges))
	for i, r := range ranges {
		events := make([]advEventJSON, 0, len(r.Events))
		for j, e := range r.Events {
			set := 0
			for _, v := range []string{e.Introduced, e.Fixed, e.LastAffected, e.Limit} {
				if v != "" {
					set++
				}
			}
			if set != 1 {
				return "", fmt.Errorf("range %d event %d sets %d fields, want exactly 1", i, j, set)
			}
			events = append(events, advEventJSON(e))
		}
		out = append(out, advRangeJSON{Type: string(r.Type), Repo: r.Repo, Events: events})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encode ranges: %w", err)
	}
	return string(b), nil
}

// advDecodeRanges reads a stored ranges column; NULL and "[]" become nil.
func advDecodeRanges(s sql.NullString) ([]model.AffectedRange, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	var in []advRangeJSON
	if err := json.Unmarshal([]byte(s.String), &in); err != nil {
		return nil, fmt.Errorf("decode ranges: %w", err)
	}
	var out []model.AffectedRange
	for _, r := range in {
		var events []model.RangeEvent
		for _, e := range r.Events {
			events = append(events, model.RangeEvent(e))
		}
		out = append(out, model.AffectedRange{Type: model.RangeType(r.Type), Repo: r.Repo, Events: events})
	}
	return out, nil
}

// advEncodeStrings renders a string list as a JSON array, "[]" when empty.
func advEncodeStrings(list []string) (string, error) {
	if list == nil {
		list = []string{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// advDecodeStrings reads a stored JSON string array; NULL and "[]" become nil.
func advDecodeStrings(s sql.NullString) ([]string, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(s.String), &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list, nil
}

// advNullBytes stores nil bytes as NULL and anything else as text.
func advNullBytes(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}

// advColumns selects an advisory without its raw record.
const advColumns = `id, aliases, summary, details, severity_label, cvss_score, cvss_vector,
	published_ts, modified_ts, withdrawn_ts, kev, kev_added_ts, epss, epss_percentile, epss_ts`

// advScan reads one row selected with advColumns.
func advScan(s scanner) (model.Advisory, error) {
	var (
		a                                      model.Advisory
		aliases, summary, details, severity    sql.NullString
		vector, published, modified, withdrawn sql.NullString
		kevAdded, epssAt                       sql.NullString
		cvss, epss, epssPercentile             sql.NullFloat64
		kev                                    int64
	)
	if err := s.Scan(&a.ID, &aliases, &summary, &details, &severity, &cvss, &vector,
		&published, &modified, &withdrawn, &kev, &kevAdded, &epss, &epssPercentile, &epssAt); err != nil {
		return model.Advisory{}, err
	}
	a.Summary = summary.String
	a.Details = details.String
	a.Severity = model.SeverityLabel(severity.String)
	a.CVSSScore = cvss.Float64
	a.CVSSVector = vector.String
	a.KEV = kev != 0
	a.HasEPSS = epss.Valid
	a.EPSS = epss.Float64
	a.EPSSPercentile = epssPercentile.Float64
	var err error
	if a.Aliases, err = advDecodeStrings(aliases); err != nil {
		return model.Advisory{}, fmt.Errorf("advisory %s aliases: %w", a.ID, err)
	}
	times := []struct {
		col string
		src sql.NullString
		dst *time.Time
	}{
		{"published_ts", published, &a.Published},
		{"modified_ts", modified, &a.Modified},
		{"withdrawn_ts", withdrawn, &a.Withdrawn},
		{"kev_added_ts", kevAdded, &a.KEVAddedAt},
		{"epss_ts", epssAt, &a.EPSSAt},
	}
	for _, t := range times {
		v, err := parseTime(t.src)
		if err != nil {
			return model.Advisory{}, fmt.Errorf("advisory %s %s: %w", a.ID, t.col, err)
		}
		*t.dst = v
	}
	return a, nil
}

// advAffectedColumns selects an affected entry with its advisory ID and package.
// Queries using it read "FROM advisory_affected aa JOIN packages p ON p.id =
// aa.package_id".
const advAffectedColumns = `aa.advisory_id, p.purl, p.ecosystem, p.name, aa.ranges, aa.versions,
	aa.ecosystem_specific, aa.source_db`

// advScanAffected reads one row selected with advAffectedColumns.
func advScanAffected(s scanner) (string, model.AffectedPackage, error) {
	var (
		advisoryID                                    string
		ap                                            model.AffectedPackage
		ecosystem, name, ranges, versions, eco, srcDB sql.NullString
	)
	if err := s.Scan(&advisoryID, &ap.Package.PURL, &ecosystem, &name, &ranges, &versions,
		&eco, &srcDB); err != nil {
		return "", model.AffectedPackage{}, err
	}
	ap.Package.Ecosystem = ecosystem.String
	ap.Package.Name = name.String
	ap.SourceDB = srcDB.String
	if eco.Valid {
		ap.EcosystemSpecific = []byte(eco.String)
	}
	var err error
	if ap.Ranges, err = advDecodeRanges(ranges); err != nil {
		return "", model.AffectedPackage{}, fmt.Errorf("advisory %s affected %s: %w", advisoryID, ap.Package.PURL, err)
	}
	if ap.Versions, err = advDecodeStrings(versions); err != nil {
		return "", model.AffectedPackage{}, fmt.Errorf("advisory %s affected %s versions: %w", advisoryID, ap.Package.PURL, err)
	}
	return advisoryID, ap, nil
}

// advList reads advisories and their affected entries, both
// filtered by the same optional advisory ID (empty means all), advisories
// ordered by id and affected entries by row ID. It must run inside advRead.
func (db *DB) advList(ctx context.Context, id string) ([]model.Advisory, error) {
	advQuery := `SELECT ` + advColumns + ` FROM advisories`
	affQuery := `SELECT ` + advAffectedColumns + `
		FROM advisory_affected aa JOIN packages p ON p.id = aa.package_id`
	var args []any
	if id != "" {
		advQuery += ` WHERE id = ?`
		affQuery += ` WHERE aa.advisory_id = ?`
		args = []any{id}
	}
	advQuery += ` ORDER BY id`
	affQuery += ` ORDER BY aa.id`

	rows, err := db.q.QueryContext(ctx, advQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("read advisories: %w", err)
	}
	defer rows.Close()
	var advisories []model.Advisory
	index := map[string]int{}
	for rows.Next() {
		a, err := advScan(rows)
		if err != nil {
			return nil, fmt.Errorf("read advisories: %w", err)
		}
		index[a.ID] = len(advisories)
		advisories = append(advisories, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read advisories: %w", err)
	}
	if len(advisories) == 0 {
		return nil, nil
	}

	affRows, err := db.q.QueryContext(ctx, affQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("read affected packages: %w", err)
	}
	defer affRows.Close()
	for affRows.Next() {
		advisoryID, ap, err := advScanAffected(affRows)
		if err != nil {
			return nil, fmt.Errorf("read affected packages: %w", err)
		}
		if i, ok := index[advisoryID]; ok {
			advisories[i].Affected = append(advisories[i].Affected, ap)
		}
	}
	if err := affRows.Err(); err != nil {
		return nil, fmt.Errorf("read affected packages: %w", err)
	}
	return advisories, nil
}

// ListAdvisories returns every stored advisory, withdrawn ones included, with
// its Affected entries in insertion order and its exploit signals, Raw left
// nil, ordered by id. It returns nil when there are none.
func (db *DB) ListAdvisories(ctx context.Context) ([]model.Advisory, error) {
	var out []model.Advisory
	err := db.advRead(ctx, func(r *DB) error {
		var err error
		out, err = r.advList(ctx, "")
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list advisories: %w", err)
	}
	return out, nil
}

// AdvisoryModified returns the stored modified time of each id that is stored
// (the zero time when the advisory has none); ids not stored are absent. Long
// lists are queried in chunks.
func (db *DB) AdvisoryModified(ctx context.Context, ids []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for chunk := range slices.Chunk(ids, advChunk) {
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		// Only the placeholder list is generated; every value is bound.
		rows, err := db.q.QueryContext(ctx,
			`SELECT id, modified_ts FROM advisories WHERE id IN (`+advPlaceholders(len(chunk))+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("read advisory modified times: %w", err)
		}
		err = func() error {
			defer rows.Close()
			for rows.Next() {
				var (
					id       string
					modified sql.NullString
				)
				if err := rows.Scan(&id, &modified); err != nil {
					return err
				}
				t, err := parseTime(modified)
				if err != nil {
					return fmt.Errorf("advisory %s modified_ts: %w", id, err)
				}
				out[id] = t
			}
			return rows.Err()
		}()
		if err != nil {
			return nil, fmt.Errorf("read advisory modified times: %w", err)
		}
	}
	return out, nil
}

// advPlaceholders returns n comma-separated "?" placeholders.
func advPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// SaveAdvisory upserts a's advisories row and replaces its advisory_affected
// rows with a.Affected, in one transaction. A new row takes every column from
// a; an existing row keeps its exploit signals (kev, kev_added_ts, epss,
// epss_percentile, epss_ts) and created time and takes every other column from
// a. A package named by a.Affected that is not stored yet is inserted with its
// ecosystem and name; an existing one keeps its name and only gains an
// ecosystem it lacked. Every range event must set exactly one field.
func (db *DB) SaveAdvisory(ctx context.Context, a model.Advisory) error {
	if a.ID == "" {
		return errors.New("save advisory: empty id")
	}
	err := db.InTx(ctx, func(tx *DB) error {
		now := formatTime(tx.now())
		aliases, err := advEncodeStrings(a.Aliases)
		if err != nil {
			return fmt.Errorf("encode aliases: %w", err)
		}
		var cvss, epss, epssPercentile any
		if a.CVSSVector != "" {
			cvss = a.CVSSScore
		}
		if a.HasEPSS {
			epss, epssPercentile = a.EPSS, a.EPSSPercentile
		}
		kev := 0
		if a.KEV {
			kev = 1
		}
		if _, err := tx.q.ExecContext(ctx,
			`INSERT INTO advisories (id, aliases, summary, details, severity_label, cvss_score,
				cvss_vector, published_ts, modified_ts, withdrawn_ts, kev, kev_added_ts, epss,
				epss_percentile, epss_ts, raw, created_ts, updated_ts)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (id) DO UPDATE SET
				aliases = excluded.aliases,
				summary = excluded.summary,
				details = excluded.details,
				severity_label = excluded.severity_label,
				cvss_score = excluded.cvss_score,
				cvss_vector = excluded.cvss_vector,
				published_ts = excluded.published_ts,
				modified_ts = excluded.modified_ts,
				withdrawn_ts = excluded.withdrawn_ts,
				raw = excluded.raw,
				updated_ts = excluded.updated_ts`,
			a.ID, aliases, nullText(a.Summary), nullText(a.Details), nullText(string(a.Severity)), cvss,
			nullText(a.CVSSVector), formatTime(a.Published), formatTime(a.Modified), formatTime(a.Withdrawn),
			kev, formatTime(a.KEVAddedAt), epss, epssPercentile, formatTime(a.EPSSAt), advNullBytes(a.Raw),
			now, now); err != nil {
			return fmt.Errorf("write advisory: %w", err)
		}
		if _, err := tx.q.ExecContext(ctx,
			`DELETE FROM advisory_affected WHERE advisory_id = ?`, a.ID); err != nil {
			return fmt.Errorf("delete affected packages: %w", err)
		}
		for i, ap := range a.Affected {
			if err := tx.advInsertAffected(ctx, a.ID, ap, now); err != nil {
				return fmt.Errorf("affected entry %d (%s): %w", i, ap.Package.PURL, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save advisory %s: %w", a.ID, err)
	}
	return nil
}

// advInsertAffected inserts one advisory_affected row, upserting its package
// the way ApplyTimeline does.
func (db *DB) advInsertAffected(ctx context.Context, advisoryID string, ap model.AffectedPackage, now any) error {
	if ap.Package.PURL == "" {
		return errors.New("empty package URL")
	}
	ranges, err := advEncodeRanges(ap.Ranges)
	if err != nil {
		return err
	}
	versions, err := advEncodeStrings(ap.Versions)
	if err != nil {
		return fmt.Errorf("encode versions: %w", err)
	}
	if _, err := db.q.ExecContext(ctx,
		`INSERT INTO packages (purl, ecosystem, name, created_ts, updated_ts)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (purl) DO UPDATE SET
			ecosystem = excluded.ecosystem,
			updated_ts = excluded.updated_ts
		 WHERE (ecosystem IS NULL OR ecosystem = '') AND excluded.ecosystem IS NOT NULL`,
		ap.Package.PURL, nullText(ap.Package.Ecosystem), nullText(ap.Package.Name), now, now); err != nil {
		return fmt.Errorf("upsert package: %w", err)
	}
	var packageID int64
	if err := db.q.QueryRowContext(ctx,
		`SELECT id FROM packages WHERE purl = ?`, ap.Package.PURL).Scan(&packageID); err != nil {
		return fmt.Errorf("look up package: %w", err)
	}
	if _, err := db.q.ExecContext(ctx,
		`INSERT INTO advisory_affected (advisory_id, package_id, ranges, versions, ecosystem_specific,
			source_db, created_ts, updated_ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		advisoryID, packageID, ranges, versions, advNullBytes(ap.EcosystemSpecific),
		nullText(ap.SourceDB), now, now); err != nil {
		return fmt.Errorf("insert affected package: %w", err)
	}
	return nil
}

// advSignalUpdate is what the signals of one ApplySignals call set on one
// advisory.
type advSignalUpdate struct {
	kev            bool
	kevAdded       time.Time
	epss           bool
	epssScore      float64
	epssPercentile float64
}

// ApplySignals stores signals on every advisory whose id or one of whose
// aliases equals the signal's CVE, in one transaction. A KEV signal sets kev =
// 1 and kev_added_ts (NULL when KEVAddedAt is zero); an EPSS signal sets epss,
// epss_percentile and epss_ts = at. When several signals of one kind reach the
// same advisory, the last one wins. Signals for CVEs no stored advisory names
// are ignored, and a signal of an unknown kind is an error. It returns the
// number of distinct advisories updated.
func (db *DB) ApplySignals(ctx context.Context, signals []model.Signal, at time.Time) (int, error) {
	if len(signals) == 0 {
		return 0, nil
	}
	var updated int
	err := db.InTx(ctx, func(tx *DB) error {
		byCVE, err := tx.advByCVE(ctx)
		if err != nil {
			return err
		}
		updates := map[string]*advSignalUpdate{}
		for _, s := range signals {
			if s.Kind != model.SignalKEV && s.Kind != model.SignalEPSS {
				return fmt.Errorf("signal for %s has unknown kind %q", s.CVE, s.Kind)
			}
			for _, id := range byCVE[s.CVE] {
				u := updates[id]
				if u == nil {
					u = &advSignalUpdate{}
					updates[id] = u
				}
				if s.Kind == model.SignalKEV {
					u.kev, u.kevAdded = true, s.KEVAddedAt
				} else {
					u.epss, u.epssScore, u.epssPercentile = true, s.EPSS, s.EPSSPercentile
				}
			}
		}
		ids := make([]string, 0, len(updates))
		for id := range updates {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		now := formatTime(tx.now())
		epssAt := formatTime(at)
		for _, id := range ids {
			u := updates[id]
			if u.kev {
				if _, err := tx.q.ExecContext(ctx,
					`UPDATE advisories SET kev = 1, kev_added_ts = ?, updated_ts = ? WHERE id = ?`,
					formatTime(u.kevAdded), now, id); err != nil {
					return fmt.Errorf("store KEV signal on %s: %w", id, err)
				}
			}
			if u.epss {
				if _, err := tx.q.ExecContext(ctx,
					`UPDATE advisories SET epss = ?, epss_percentile = ?, epss_ts = ?, updated_ts = ? WHERE id = ?`,
					u.epssScore, u.epssPercentile, epssAt, now, id); err != nil {
					return fmt.Errorf("store EPSS signal on %s: %w", id, err)
				}
			}
		}
		updated = len(ids)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("apply signals: %w", err)
	}
	return updated, nil
}

// advByCVE maps every id and alias of every stored advisory to the IDs of the
// advisories that carry it.
func (db *DB) advByCVE(ctx context.Context) (map[string][]string, error) {
	rows, err := db.q.QueryContext(ctx, `SELECT id, aliases FROM advisories ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read advisory aliases: %w", err)
	}
	defer rows.Close()
	byCVE := map[string][]string{}
	for rows.Next() {
		var (
			id      string
			aliases sql.NullString
		)
		if err := rows.Scan(&id, &aliases); err != nil {
			return nil, fmt.Errorf("read advisory aliases: %w", err)
		}
		list, err := advDecodeStrings(aliases)
		if err != nil {
			return nil, fmt.Errorf("advisory %s aliases: %w", id, err)
		}
		for _, name := range append([]string{id}, list...) {
			if !slices.Contains(byCVE[name], id) {
				byCVE[name] = append(byCVE[name], id)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read advisory aliases: %w", err)
	}
	return byCVE, nil
}

// QueriedVersions returns every pair already sent to the advisory database,
// ordered by package URL, then version. It returns nil when there are none.
func (db *DB) QueriedVersions(ctx context.Context) ([]model.PackageVersion, error) {
	rows, err := db.q.QueryContext(ctx, `SELECT purl, version FROM osv_queries ORDER BY purl, version`)
	if err != nil {
		return nil, fmt.Errorf("list queried versions: %w", err)
	}
	defer rows.Close()
	var out []model.PackageVersion
	for rows.Next() {
		var pv model.PackageVersion
		if err := rows.Scan(&pv.PURL, &pv.Version); err != nil {
			return nil, fmt.Errorf("list queried versions: %w", err)
		}
		out = append(out, pv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list queried versions: %w", err)
	}
	return out, nil
}

// MarkQueried records pvs as queried at the given time, in one transaction. A
// pair already recorded keeps its row and gets the new queried time.
func (db *DB) MarkQueried(ctx context.Context, pvs []model.PackageVersion, at time.Time) error {
	if len(pvs) == 0 {
		return nil
	}
	err := db.InTx(ctx, func(tx *DB) error {
		now := formatTime(tx.now())
		queried := formatTime(at)
		for _, pv := range pvs {
			if _, err := tx.q.ExecContext(ctx,
				`INSERT INTO osv_queries (purl, version, queried_ts, created_ts, updated_ts)
				 VALUES (?, ?, ?, ?, ?)
				 ON CONFLICT (purl, version) DO UPDATE SET
					queried_ts = excluded.queried_ts,
					updated_ts = excluded.updated_ts`,
				pv.PURL, pv.Version, queried, now, now); err != nil {
				return fmt.Errorf("mark %s@%s: %w", pv.PURL, pv.Version, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("mark queried versions: %w", err)
	}
	return nil
}

// advMatchColumns selects an advisory match. Queries using it read "FROM
// advisory_matches am".
const advMatchColumns = `am.id, am.repo_id, am.dep_version_id, am.advisory_id, am.exposed_from_ts,
	am.exposed_to_ts, am.fixed_version, am.confidence, am.confidence_reason`

// advMatchRow holds the scan destinations of advMatchColumns.
type advMatchRow struct {
	m                             model.AdvisoryMatch
	from, to, fixed, conf, reason sql.NullString
}

// dests returns the scan destinations in advMatchColumns order.
func (r *advMatchRow) dests() []any {
	return []any{&r.m.ID, &r.m.RepoID, &r.m.DepVersionID, &r.m.AdvisoryID, &r.from, &r.to,
		&r.fixed, &r.conf, &r.reason}
}

// match converts the scanned row to a model value.
func (r *advMatchRow) match() (model.AdvisoryMatch, error) {
	m := r.m
	m.FixedVersion = r.fixed.String
	m.Confidence = model.Confidence(r.conf.String)
	m.ConfidenceReason = r.reason.String
	var err error
	if m.ExposedFrom, err = parseTime(r.from); err != nil {
		return model.AdvisoryMatch{}, fmt.Errorf("match %d exposed_from_ts: %w", m.ID, err)
	}
	if m.ExposedTo, err = parseTime(r.to); err != nil {
		return model.AdvisoryMatch{}, fmt.Errorf("match %d exposed_to_ts: %w", m.ID, err)
	}
	return m, nil
}

// advMatchKey identifies an advisory match.
type advMatchKey struct {
	depVersionID int64
	advisoryID   string
}

// ReplaceMatches makes matches the complete set of advisory matches for
// repoID, in one transaction. Every match must have RepoID equal to repoID, a
// non-empty AdvisoryID and a non-zero DepVersionID, and no two may share a
// (DepVersionID, AdvisoryID) pair; otherwise it returns an error and writes
// nothing. A row whose pair is in matches keeps its ID and has every column
// rewritten from the match; missing pairs are inserted, and the repo's other
// rows are deleted.
func (db *DB) ReplaceMatches(ctx context.Context, repoID int64, matches []model.AdvisoryMatch) error {
	wanted := make(map[advMatchKey]bool, len(matches))
	for i, m := range matches {
		switch {
		case m.RepoID != repoID:
			return fmt.Errorf("replace matches of repo %d: match %d has repo %d", repoID, i, m.RepoID)
		case m.AdvisoryID == "":
			return fmt.Errorf("replace matches of repo %d: match %d has no advisory", repoID, i)
		case m.DepVersionID == 0:
			return fmt.Errorf("replace matches of repo %d: match %d has no interval", repoID, i)
		}
		k := advMatchKey{m.DepVersionID, m.AdvisoryID}
		if wanted[k] {
			return fmt.Errorf("replace matches of repo %d: advisory %s on interval %d appears twice",
				repoID, m.AdvisoryID, m.DepVersionID)
		}
		wanted[k] = true
	}
	err := db.InTx(ctx, func(tx *DB) error {
		rows, err := tx.q.QueryContext(ctx,
			`SELECT id, dep_version_id, advisory_id FROM advisory_matches WHERE repo_id = ? ORDER BY id`, repoID)
		if err != nil {
			return fmt.Errorf("read stored matches: %w", err)
		}
		var vanished []int64
		err = func() error {
			defer rows.Close()
			for rows.Next() {
				var (
					id int64
					k  advMatchKey
				)
				if err := rows.Scan(&id, &k.depVersionID, &k.advisoryID); err != nil {
					return err
				}
				if !wanted[k] {
					vanished = append(vanished, id)
				}
			}
			return rows.Err()
		}()
		if err != nil {
			return fmt.Errorf("read stored matches: %w", err)
		}
		for _, id := range vanished {
			if _, err := tx.q.ExecContext(ctx, `DELETE FROM advisory_matches WHERE id = ?`, id); err != nil {
				return fmt.Errorf("delete match %d: %w", id, err)
			}
		}
		now := formatTime(tx.now())
		for _, m := range matches {
			if _, err := tx.q.ExecContext(ctx,
				`INSERT INTO advisory_matches (repo_id, dep_version_id, advisory_id, exposed_from_ts,
					exposed_to_ts, fixed_version, confidence, confidence_reason, created_ts, updated_ts)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT (dep_version_id, advisory_id) DO UPDATE SET
					repo_id = excluded.repo_id,
					exposed_from_ts = excluded.exposed_from_ts,
					exposed_to_ts = excluded.exposed_to_ts,
					fixed_version = excluded.fixed_version,
					confidence = excluded.confidence,
					confidence_reason = excluded.confidence_reason,
					updated_ts = excluded.updated_ts`,
				m.RepoID, m.DepVersionID, m.AdvisoryID, formatTime(m.ExposedFrom), formatTime(m.ExposedTo),
				nullText(m.FixedVersion), nullText(string(m.Confidence)), nullText(m.ConfidenceReason),
				now, now); err != nil {
				return fmt.Errorf("write match of %s on interval %d: %w", m.AdvisoryID, m.DepVersionID, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("replace matches of repo %d: %w", repoID, err)
	}
	return nil
}

// ListAllIntervals returns every stored dependency interval of every repo,
// open and closed, with its repo and package IDs and the interval filled as
// ListIntervals fills it, ordered by repo slug, package URL, manifest path,
// opening time, then ID. It returns nil when there are none.
func (db *DB) ListAllIntervals(ctx context.Context) ([]model.RepoInterval, error) {
	rows, err := db.q.QueryContext(ctx,
		`SELECT v.repo_id, r.slug, v.package_id, `+intervalColumns+`
		 FROM dep_versions v
		 JOIN repos r ON r.id = v.repo_id
		 JOIN manifests m ON m.id = v.manifest_id
		 JOIN packages p ON p.id = v.package_id
		 LEFT JOIN commits fc ON fc.id = v.from_commit_id
		 LEFT JOIN commits tc ON tc.id = v.to_commit_id
		 ORDER BY r.slug, p.purl, m.path, v.from_ts, v.id`)
	if err != nil {
		return nil, fmt.Errorf("list all intervals: %w", err)
	}
	defer rows.Close()
	var out []model.RepoInterval
	for rows.Next() {
		var ri model.RepoInterval
		iv, err := scanInterval(advPrefixScanner{s: rows, prefix: []any{&ri.RepoID, &ri.RepoSlug, &ri.PackageID}})
		if err != nil {
			return nil, fmt.Errorf("list all intervals: %w", err)
		}
		ri.Interval = iv
		out = append(out, ri)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all intervals: %w", err)
	}
	return out, nil
}
