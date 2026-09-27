-- Per-track record of rejected candidate source keys with a reason and a
-- timestamp, so a retry can skip a key rejected inside its expiry window
-- instead of walking it again. tracks.id is UUID (migrations/001_baseline.sql)
-- but track_id here is TEXT with no FK, matching acquisition_outcomes
-- (migrations/026_acquisition_outcomes.sql): a rejection may reference a track
-- that acquisition later discards, and neither table needs the row gone
-- before it can be read. Re-recording the same (track_id, source_key) upserts
-- reason, detail and rejected_at.
CREATE TABLE IF NOT EXISTS acquisition_rejections (
    track_id    TEXT        NOT NULL CHECK (track_id <> ''),
    source_key  TEXT        NOT NULL CHECK (source_key <> ''),
    reason      TEXT        NOT NULL,
    detail      TEXT        NOT NULL DEFAULT '',
    rejected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (track_id, source_key)
);
CREATE INDEX IF NOT EXISTS idx_acquisition_rejections_rejected_at ON acquisition_rejections (rejected_at);
