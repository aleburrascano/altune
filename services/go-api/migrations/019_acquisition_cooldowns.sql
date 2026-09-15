-- Last manual retry/reacquire admission per track. The cooldown gate reads and
-- writes this table in one atomic upsert, so the "one retry/reacquire per track
-- per window" rule holds across process restarts and go-api replicas instead of
-- living in a per-process map (#986). One row per (track, kind); the row is
-- overwritten on each admission and removed with its track.
CREATE TABLE IF NOT EXISTS acquisition_cooldowns (
    track_id    UUID        NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL CHECK (kind IN ('retry', 'reacquire')),
    admitted_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (track_id, kind)
);
