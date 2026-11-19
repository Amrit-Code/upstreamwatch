-- 0002_timeline: what the M1 timeline needs beyond the initial schema.
-- dep_versions gains the requirement as written, so an interval whose
-- manifest gives only a constraint (an unpinned PyPI requirement) still shows
-- what was declared. An interval is identified by its repo, manifest, package
-- and opening commit; the unique index enforces that, and lets a rebuild keep
-- the row IDs that advisory matches and findings point at.

ALTER TABLE dep_versions ADD COLUMN requirement TEXT;

-- The view lists its columns, so it is recreated to carry the new one.
DROP VIEW dep_state;
CREATE VIEW dep_state AS
SELECT id, repo_id, package_id, manifest_id, version, requirement, direct,
       from_commit_id, from_ts, to_commit_id, to_ts, created_ts, updated_ts
FROM dep_versions
WHERE to_ts IS NULL;

CREATE UNIQUE INDEX dep_versions_identity
    ON dep_versions (repo_id, manifest_id, package_id, from_commit_id);

CREATE INDEX commits_repo_position ON commits (repo_id, position);
