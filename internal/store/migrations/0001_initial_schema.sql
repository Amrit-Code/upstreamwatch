-- 0001_initial_schema: every table in docs/database.md and the dep_state view.
-- The whole schema lands in one migration because, with foreign keys on, SQLite
-- rejects writes to a table whose REFERENCES names a table that does not exist
-- yet, and constraints cannot be added later under the additive-only rule.
-- Parent tables come before their children. A reindex deletes and rebuilds a
-- repo's dep_versions, so rows derived from an interval (advisory_matches,
-- issue_candidates) cascade with it and findings keep their row with
-- dep_version_id set to NULL; every other foreign key is NO ACTION, so code
-- deletes children before parents. Indexes cover the M0 queries and
-- each foreign-key child column a parent delete would scan; later migrations
-- add the indexes their own queries need.

-- Targets ------------------------------------------------------------------

CREATE TABLE repos (
    id               INTEGER PRIMARY KEY,
    slug             TEXT NOT NULL UNIQUE,
    url              TEXT,
    default_branch   TEXT,
    clone_path       TEXT,
    last_indexed_sha TEXT,
    last_fetch_ts    TEXT,
    last_fetch_error TEXT,
    created_ts       TEXT NOT NULL,
    updated_ts       TEXT NOT NULL
);

CREATE TABLE commits (
    id           INTEGER PRIMARY KEY,
    repo_id      INTEGER NOT NULL REFERENCES repos (id),
    sha          TEXT NOT NULL,
    position     INTEGER,
    committed_ts TEXT,
    author_name  TEXT,
    author_email TEXT,
    subject      TEXT,
    created_ts   TEXT NOT NULL,
    updated_ts   TEXT NOT NULL,
    UNIQUE (repo_id, sha)
);

CREATE TABLE manifests (
    id         INTEGER PRIMARY KEY,
    repo_id    INTEGER NOT NULL REFERENCES repos (id),
    path       TEXT NOT NULL,
    ecosystem  TEXT,
    kind       TEXT,
    created_ts TEXT NOT NULL,
    updated_ts TEXT NOT NULL,
    UNIQUE (repo_id, path)
);

-- Packages -----------------------------------------------------------------

CREATE TABLE packages (
    id         INTEGER PRIMARY KEY,
    purl       TEXT NOT NULL UNIQUE,
    ecosystem  TEXT,
    name       TEXT,
    created_ts TEXT NOT NULL,
    updated_ts TEXT NOT NULL
);

-- Timeline -----------------------------------------------------------------

CREATE TABLE dep_events (
    id           INTEGER PRIMARY KEY,
    repo_id      INTEGER NOT NULL REFERENCES repos (id),
    commit_id    INTEGER NOT NULL REFERENCES commits (id),
    manifest_id  INTEGER NOT NULL REFERENCES manifests (id),
    package_id   INTEGER NOT NULL REFERENCES packages (id),
    change       TEXT,
    version      TEXT,
    prev_version TEXT,
    requirement  TEXT,
    direct       INTEGER,
    scope        TEXT,
    created_ts   TEXT NOT NULL,
    updated_ts   TEXT NOT NULL,
    UNIQUE (commit_id, manifest_id, package_id)
);
CREATE INDEX dep_events_repo_id ON dep_events (repo_id);
CREATE INDEX dep_events_manifest_id ON dep_events (manifest_id);
CREATE INDEX dep_events_package_id ON dep_events (package_id);

CREATE TABLE dep_versions (
    id             INTEGER PRIMARY KEY,
    repo_id        INTEGER NOT NULL REFERENCES repos (id),
    package_id     INTEGER NOT NULL REFERENCES packages (id),
    manifest_id    INTEGER NOT NULL REFERENCES manifests (id),
    version        TEXT,
    direct         INTEGER,
    from_commit_id INTEGER REFERENCES commits (id),
    from_ts        TEXT,
    to_commit_id   INTEGER REFERENCES commits (id),
    to_ts          TEXT,
    created_ts     TEXT NOT NULL,
    updated_ts     TEXT NOT NULL
);
CREATE INDEX dep_versions_repo_id ON dep_versions (repo_id);
CREATE INDEX dep_versions_package_id ON dep_versions (package_id);
CREATE INDEX dep_versions_manifest_id ON dep_versions (manifest_id);
CREATE INDEX dep_versions_from_commit_id ON dep_versions (from_commit_id);
CREATE INDEX dep_versions_to_commit_id ON dep_versions (to_commit_id);

CREATE VIEW dep_state AS
SELECT id, repo_id, package_id, manifest_id, version, direct,
       from_commit_id, from_ts, to_commit_id, to_ts, created_ts, updated_ts
FROM dep_versions
WHERE to_ts IS NULL;

-- Target usage index -------------------------------------------------------

CREATE TABLE symbol_usage (
    id          INTEGER PRIMARY KEY,
    repo_id     INTEGER NOT NULL REFERENCES repos (id),
    package_id  INTEGER NOT NULL REFERENCES packages (id),
    indexed_sha TEXT,
    symbol      TEXT,
    file        TEXT,
    line        INTEGER,
    count       INTEGER,
    created_ts  TEXT NOT NULL,
    updated_ts  TEXT NOT NULL
);
CREATE INDEX symbol_usage_repo_id ON symbol_usage (repo_id);
CREATE INDEX symbol_usage_package_id ON symbol_usage (package_id);

-- Upstream mapping ---------------------------------------------------------

CREATE TABLE upstream_repos (
    id                 INTEGER PRIMARY KEY,
    host               TEXT,
    owner              TEXT,
    name               TEXT,
    url                TEXT NOT NULL UNIQUE,
    issue_source       TEXT,
    watch_issues       INTEGER NOT NULL DEFAULT 1,
    issues_cursor_ts   TEXT,
    comments_cursor_ts TEXT,
    issues_etag        TEXT,
    comments_etag      TEXT,
    last_poll_ts       TEXT,
    last_poll_status   INTEGER,
    backfilled_to_ts   TEXT,
    created_ts         TEXT NOT NULL,
    updated_ts         TEXT NOT NULL
);

CREATE TABLE package_upstream (
    package_id       INTEGER NOT NULL REFERENCES packages (id),
    upstream_repo_id INTEGER NOT NULL REFERENCES upstream_repos (id),
    relation         TEXT NOT NULL,
    provenance       TEXT,
    confidence       REAL,
    resolved_ts      TEXT,
    created_ts       TEXT NOT NULL,
    updated_ts       TEXT NOT NULL,
    PRIMARY KEY (package_id, upstream_repo_id, relation)
);
CREATE INDEX package_upstream_upstream_repo_id ON package_upstream (upstream_repo_id);

-- Advisories ---------------------------------------------------------------

CREATE TABLE advisories (
    id              TEXT NOT NULL PRIMARY KEY,
    aliases         TEXT,
    summary         TEXT,
    details         TEXT,
    severity_label  TEXT,
    cvss_score      REAL,
    cvss_vector     TEXT,
    published_ts    TEXT,
    modified_ts     TEXT,
    withdrawn_ts    TEXT,
    kev             INTEGER NOT NULL DEFAULT 0,
    kev_added_ts    TEXT,
    epss            REAL,
    epss_percentile REAL,
    epss_ts         TEXT,
    raw             TEXT,
    created_ts      TEXT NOT NULL,
    updated_ts      TEXT NOT NULL
);

CREATE TABLE advisory_affected (
    id                 INTEGER PRIMARY KEY,
    advisory_id        TEXT NOT NULL REFERENCES advisories (id),
    package_id         INTEGER NOT NULL REFERENCES packages (id),
    ranges             TEXT,
    versions           TEXT,
    ecosystem_specific TEXT,
    source_db          TEXT,
    created_ts         TEXT NOT NULL,
    updated_ts         TEXT NOT NULL
);
CREATE INDEX advisory_affected_advisory_id ON advisory_affected (advisory_id);
CREATE INDEX advisory_affected_package_id ON advisory_affected (package_id);

CREATE TABLE advisory_matches (
    id                INTEGER PRIMARY KEY,
    repo_id           INTEGER NOT NULL REFERENCES repos (id),
    dep_version_id    INTEGER NOT NULL REFERENCES dep_versions (id) ON DELETE CASCADE,
    advisory_id       TEXT NOT NULL REFERENCES advisories (id),
    exposed_from_ts   TEXT,
    exposed_to_ts     TEXT,
    fixed_version     TEXT,
    confidence        TEXT,
    confidence_reason TEXT,
    created_ts        TEXT NOT NULL,
    updated_ts        TEXT NOT NULL,
    UNIQUE (dep_version_id, advisory_id)
);
CREATE INDEX advisory_matches_repo_id ON advisory_matches (repo_id);
CREATE INDEX advisory_matches_advisory_id ON advisory_matches (advisory_id);

-- Upstream issues ----------------------------------------------------------

CREATE TABLE upstream_issues (
    id                  INTEGER PRIMARY KEY,
    upstream_repo_id    INTEGER NOT NULL REFERENCES upstream_repos (id),
    number              INTEGER NOT NULL,
    external_id         TEXT,
    is_pull             INTEGER,
    state               TEXT,
    title               TEXT,
    body                TEXT,
    labels              TEXT,
    author              TEXT,
    author_assoc        TEXT,
    upstream_created_ts TEXT,
    upstream_updated_ts TEXT,
    closed_ts           TEXT,
    comments_count      INTEGER,
    body_hash           TEXT,
    html_url            TEXT,
    created_ts          TEXT NOT NULL,
    updated_ts          TEXT NOT NULL,
    UNIQUE (upstream_repo_id, number)
);

CREATE TABLE upstream_issue_comments (
    id                  INTEGER PRIMARY KEY,
    issue_id            INTEGER NOT NULL REFERENCES upstream_issues (id),
    external_id         TEXT NOT NULL,
    author              TEXT,
    body                TEXT,
    upstream_created_ts TEXT,
    upstream_updated_ts TEXT,
    created_ts          TEXT NOT NULL,
    updated_ts          TEXT NOT NULL,
    UNIQUE (issue_id, external_id)
);

-- Prefilter and LLM --------------------------------------------------------

CREATE TABLE issue_candidates (
    id              INTEGER PRIMARY KEY,
    issue_id        INTEGER NOT NULL REFERENCES upstream_issues (id),
    repo_id         INTEGER NOT NULL REFERENCES repos (id),
    package_id      INTEGER NOT NULL REFERENCES packages (id),
    dep_version_id  INTEGER REFERENCES dep_versions (id) ON DELETE CASCADE,
    score           REAL,
    features        TEXT,
    passed          INTEGER,
    scored_ts       TEXT,
    issue_body_hash TEXT NOT NULL,
    created_ts      TEXT NOT NULL,
    updated_ts      TEXT NOT NULL,
    UNIQUE (issue_id, repo_id, package_id, issue_body_hash)
);
CREATE INDEX issue_candidates_repo_id ON issue_candidates (repo_id);
CREATE INDEX issue_candidates_package_id ON issue_candidates (package_id);
CREATE INDEX issue_candidates_dep_version_id ON issue_candidates (dep_version_id);

CREATE TABLE llm_verdicts (
    id                 INTEGER PRIMARY KEY,
    cache_key          TEXT NOT NULL UNIQUE,
    issue_id           INTEGER NOT NULL REFERENCES upstream_issues (id),
    repo_id            INTEGER NOT NULL REFERENCES repos (id),
    package_id         INTEGER NOT NULL REFERENCES packages (id),
    pinned_version     TEXT,
    provider           TEXT,
    model              TEXT,
    prompt_version     TEXT,
    snippet_mode       TEXT,
    verdict            TEXT,
    confidence         REAL,
    justification      TEXT,
    reasoning          TEXT,
    evidence           TEXT,
    evidence_verified  INTEGER,
    fixed_in           TEXT,
    recommended_action TEXT,
    input_tokens       INTEGER,
    output_tokens      INTEGER,
    cost_usd           REAL,
    latency_ms         INTEGER,
    request_payload    TEXT,
    raw_response       TEXT,
    created_ts         TEXT NOT NULL,
    updated_ts         TEXT NOT NULL
);
CREATE INDEX llm_verdicts_issue_id ON llm_verdicts (issue_id);
CREATE INDEX llm_verdicts_repo_id ON llm_verdicts (repo_id);
CREATE INDEX llm_verdicts_package_id ON llm_verdicts (package_id);

-- Eval ---------------------------------------------------------------------

CREATE TABLE eval_labels (
    id             INTEGER PRIMARY KEY,
    issue_id       INTEGER NOT NULL REFERENCES upstream_issues (id),
    repo_id        INTEGER NOT NULL REFERENCES repos (id),
    package_id     INTEGER NOT NULL REFERENCES packages (id),
    pinned_version TEXT NOT NULL,
    label          TEXT,
    note           TEXT,
    labelled_by    TEXT,
    labelled_ts    TEXT,
    created_ts     TEXT NOT NULL,
    updated_ts     TEXT NOT NULL,
    UNIQUE (issue_id, repo_id, package_id, pinned_version)
);
CREATE INDEX eval_labels_repo_id ON eval_labels (repo_id);
CREATE INDEX eval_labels_package_id ON eval_labels (package_id);

-- Ops ----------------------------------------------------------------------

CREATE TABLE runs (
    id          INTEGER PRIMARY KEY,
    job         TEXT NOT NULL,
    started_ts  TEXT NOT NULL,
    finished_ts TEXT,
    status      TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    stats       TEXT NOT NULL,
    error       TEXT NOT NULL,
    created_ts  TEXT NOT NULL,
    updated_ts  TEXT NOT NULL
);
CREATE INDEX runs_job_id ON runs (job, id);

-- Findings -----------------------------------------------------------------

CREATE TABLE findings (
    id                    INTEGER PRIMARY KEY,
    kind                  TEXT NOT NULL CHECK (kind IN ('advisory', 'issue', 'job_failure')),
    repo_id               INTEGER REFERENCES repos (id),
    package_id            INTEGER REFERENCES packages (id),
    dep_version_id        INTEGER REFERENCES dep_versions (id) ON DELETE SET NULL,
    advisory_id           TEXT REFERENCES advisories (id),
    issue_id              INTEGER REFERENCES upstream_issues (id),
    verdict_id            INTEGER REFERENCES llm_verdicts (id),
    fingerprint           TEXT NOT NULL UNIQUE,
    score                 REAL NOT NULL,
    rank_reason           TEXT NOT NULL,
    first_seen_ts         TEXT NOT NULL,
    last_changed_ts       TEXT NOT NULL,
    resolved_ts           TEXT,
    reported_in_digest_ts TEXT,
    created_ts            TEXT NOT NULL,
    updated_ts            TEXT NOT NULL
);
CREATE INDEX findings_resolved_ts ON findings (resolved_ts);
CREATE INDEX findings_repo_id ON findings (repo_id);
CREATE INDEX findings_package_id ON findings (package_id);
CREATE INDEX findings_dep_version_id ON findings (dep_version_id);
CREATE INDEX findings_advisory_id ON findings (advisory_id);
CREATE INDEX findings_issue_id ON findings (issue_id);
CREATE INDEX findings_verdict_id ON findings (verdict_id);

CREATE TABLE finding_state (
    finding_id      INTEGER PRIMARY KEY REFERENCES findings (id),
    state           TEXT NOT NULL CHECK (state IN ('open', 'acked', 'snoozed', 'dismissed')),
    snooze_until_ts TEXT,
    note            TEXT,
    changed_ts      TEXT,
    changed_by      TEXT,
    created_ts      TEXT NOT NULL,
    updated_ts      TEXT NOT NULL
);

CREATE TABLE digests (
    id         INTEGER PRIMARY KEY,
    sent_ts    TEXT,
    channel    TEXT,
    status     TEXT,
    payload    TEXT,
    error      TEXT,
    created_ts TEXT NOT NULL,
    updated_ts TEXT NOT NULL
);

-- Settings and caches ------------------------------------------------------

CREATE TABLE settings (
    key        TEXT NOT NULL PRIMARY KEY,
    value      BLOB NOT NULL,
    updated_ts TEXT NOT NULL
);

CREATE TABLE http_cache (
    url           TEXT NOT NULL PRIMARY KEY,
    etag          TEXT,
    last_modified TEXT,
    fetched_ts    TEXT,
    status        INTEGER,
    updated_ts    TEXT NOT NULL
);

CREATE TABLE kv (
    key        TEXT NOT NULL PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_ts TEXT NOT NULL
);
