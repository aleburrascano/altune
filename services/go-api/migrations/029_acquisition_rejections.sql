CREATE TABLE IF NOT EXISTS acquisition_rejections (
    track_id    TEXT        NOT NULL CHECK (track_id <> ''),
    source_key  TEXT        NOT NULL CHECK (source_key <> ''),
    reason      TEXT        NOT NULL,
    detail      TEXT        NOT NULL DEFAULT '',
    rejected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (track_id, source_key)
);
CREATE INDEX IF NOT EXISTS idx_acquisition_rejections_rejected_at ON acquisition_rejections (rejected_at);
