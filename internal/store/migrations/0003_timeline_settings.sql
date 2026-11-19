-- 0003_timeline_settings: the fingerprint of the settings a repo's timeline
-- was built with (exclusions, ecosystems allowlist, Unity, extractor
-- version). When the current fingerprint differs, the timeline job rebuilds
-- the repo from the root, so a loosened or tightened filter or a changed
-- extractor never leaves intervals built under the old rules.

ALTER TABLE repos ADD COLUMN timeline_settings TEXT;
