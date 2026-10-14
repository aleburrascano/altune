CREATE TABLE IF NOT EXISTS track_identity (
    track_id              UUID        PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    user_id               UUID        NOT NULL,
    isrc                  TEXT,
    mb_recording_id       TEXT,
    deezer_track_id       TEXT,
    apple_song_id         TEXT,
    ytm_video_id          TEXT,
    status                TEXT        NOT NULL CHECK (status IN ('bound', 'unofficial', 'unbound')),
    reason                TEXT        NOT NULL,
    evidence              JSONB       NOT NULL,
    run_id                UUID        NOT NULL,
    bound_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    track_version_at_bind INTEGER     NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_track_identity_run ON track_identity (run_id);
CREATE INDEX IF NOT EXISTS idx_track_identity_user_status ON track_identity (user_id, status);
