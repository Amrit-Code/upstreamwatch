-- 0004_vulns: what the M2 vulnerability job needs beyond the initial schema.
-- osv_queries remembers every (package, version) pair already sent to the
-- OSV querybatch endpoint, so later runs query only pairs that are new; new
-- and changed advisories for pairs already queried arrive through the daily
-- modified_id.csv delta instead. Rows are keyed by the version-less package
-- URL so they survive a timeline rebuild.

CREATE TABLE osv_queries (
    purl       TEXT NOT NULL,
    version    TEXT NOT NULL,
    queried_ts TEXT NOT NULL,
    created_ts TEXT NOT NULL,
    updated_ts TEXT NOT NULL,
    PRIMARY KEY (purl, version)
);

CREATE INDEX digests_channel_status ON digests (channel, status);
