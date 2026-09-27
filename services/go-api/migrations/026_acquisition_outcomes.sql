CREATE TABLE IF NOT EXISTS acquisition_outcomes (
    id           BIGSERIAL   PRIMARY KEY,
    track_id     TEXT        NOT NULL CHECK (track_id <> ''),
    outcome      TEXT        NOT NULL CHECK (outcome IN ('succeeded','failed','cancelled')),
    reason       TEXT        NOT NULL DEFAULT '',
    elapsed_ms   BIGINT      NOT NULL CHECK (elapsed_ms >= 0),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_acquisition_outcomes_completed_at ON acquisition_outcomes (completed_at);
