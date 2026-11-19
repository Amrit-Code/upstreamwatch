package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// GetFindingDetail returns the finding with this ID, with its state, and what
// is needed to show it: the repo slug when it names a repo, the package when it
// names a package, the interval when it links one, and for a finding of kind
// advisory the advisory (Affected entries and exploit signals, Raw nil) and
// every match of that advisory against an interval of the finding's repo and
// package, ordered by exposure start, then interval ID. All reads see one
// snapshot. An unknown ID is an error wrapping model.ErrNotFound.
func (db *DB) GetFindingDetail(ctx context.Context, id int64) (model.FindingDetail, error) {
	var d model.FindingDetail
	err := db.advRead(ctx, func(r *DB) error {
		var err error
		d, err = r.advFindingDetail(ctx, id)
		return err
	})
	if err != nil {
		return model.FindingDetail{}, fmt.Errorf("get finding detail %d: %w", id, err)
	}
	return d, nil
}

// advFindingDetail reads the detail of one finding. It must run inside
// advRead.
func (db *DB) advFindingDetail(ctx context.Context, id int64) (model.FindingDetail, error) {
	var d model.FindingDetail
	f, err := scanFinding(db.q.QueryRowContext(ctx,
		`SELECT `+findingColumns+`
		 FROM findings f JOIN finding_state s ON s.finding_id = f.id
		 WHERE f.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("finding %d: %w", id, model.ErrNotFound)
	}
	if err != nil {
		return d, fmt.Errorf("read finding: %w", err)
	}
	d.Finding = f

	if f.RepoID != 0 {
		if err := db.q.QueryRowContext(ctx,
			`SELECT slug FROM repos WHERE id = ?`, f.RepoID).Scan(&d.RepoSlug); err != nil {
			return d, fmt.Errorf("read repo %d: %w", f.RepoID, err)
		}
	}
	if f.PackageID != 0 {
		var ecosystem, name sql.NullString
		if err := db.q.QueryRowContext(ctx,
			`SELECT purl, ecosystem, name FROM packages WHERE id = ?`, f.PackageID).Scan(
			&d.Package.PURL, &ecosystem, &name); err != nil {
			return d, fmt.Errorf("read package %d: %w", f.PackageID, err)
		}
		d.Package.Ecosystem, d.Package.Name = ecosystem.String, name.String
	}
	if f.DepVersionID != 0 {
		if d.Interval, err = scanInterval(db.q.QueryRowContext(ctx,
			`SELECT `+intervalColumns+`
			 FROM dep_versions v
			 JOIN manifests m ON m.id = v.manifest_id
			 JOIN packages p ON p.id = v.package_id
			 LEFT JOIN commits fc ON fc.id = v.from_commit_id
			 LEFT JOIN commits tc ON tc.id = v.to_commit_id
			 WHERE v.id = ?`, f.DepVersionID)); err != nil {
			return d, fmt.Errorf("read interval %d: %w", f.DepVersionID, err)
		}
	}
	if f.Kind != model.KindAdvisory || f.AdvisoryID == "" {
		return d, nil
	}

	advisories, err := db.advList(ctx, f.AdvisoryID)
	if err != nil {
		return d, fmt.Errorf("read advisory %s: %w", f.AdvisoryID, err)
	}
	if len(advisories) != 1 {
		return d, fmt.Errorf("read advisory %s: not stored", f.AdvisoryID)
	}
	d.Advisory = advisories[0]
	if d.Exposures, err = db.advExposures(ctx, f); err != nil {
		return d, err
	}
	return d, nil
}

// advExposures returns the matches of f's advisory against intervals of f's
// repo and package, each with its interval, ordered by exposure start, then
// interval ID. It returns nil when there are none.
func (db *DB) advExposures(ctx context.Context, f model.Finding) ([]model.Exposure, error) {
	rows, err := db.q.QueryContext(ctx,
		`SELECT `+advMatchColumns+`, `+intervalColumns+`
		 FROM advisory_matches am
		 JOIN dep_versions v ON v.id = am.dep_version_id
		 JOIN manifests m ON m.id = v.manifest_id
		 JOIN packages p ON p.id = v.package_id
		 LEFT JOIN commits fc ON fc.id = v.from_commit_id
		 LEFT JOIN commits tc ON tc.id = v.to_commit_id
		 WHERE am.advisory_id = ? AND am.repo_id = ? AND v.package_id = ?
		 ORDER BY am.exposed_from_ts, am.dep_version_id`,
		f.AdvisoryID, f.RepoID, f.PackageID)
	if err != nil {
		return nil, fmt.Errorf("read exposures: %w", err)
	}
	defer rows.Close()
	var out []model.Exposure
	for rows.Next() {
		var mr advMatchRow
		iv, err := scanInterval(advPrefixScanner{s: rows, prefix: mr.dests()})
		if err != nil {
			return nil, fmt.Errorf("read exposures: %w", err)
		}
		m, err := mr.match()
		if err != nil {
			return nil, fmt.Errorf("read exposures: %w", err)
		}
		out = append(out, model.Exposure{Match: m, Interval: iv})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read exposures: %w", err)
	}
	return out, nil
}
