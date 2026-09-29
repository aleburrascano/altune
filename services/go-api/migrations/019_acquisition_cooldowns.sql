CREATE TABLE IF NOT EXISTS acquisition_cooldowns (
    track_id    UUID        NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL CHECK (kind IN ('retry', 'reacquire')),
    admitted_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (track_id, kind)
);
