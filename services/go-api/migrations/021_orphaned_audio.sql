CREATE TABLE IF NOT EXISTS orphaned_audio (
    audio_ref       TEXT        PRIMARY KEY,
    user_id         UUID        NOT NULL,
    track_id        UUID        NOT NULL,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts        INTEGER     NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    last_error      TEXT
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_audio_ref
    ON tracks (audio_ref)
    WHERE audio_ref IS NOT NULL;
