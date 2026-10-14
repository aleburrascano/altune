CREATE TABLE IF NOT EXISTS track_length_fixes (
    run_id                       UUID             NOT NULL,
    track_id                     UUID             NOT NULL,
    user_id                      UUID             NOT NULL,
    action                       TEXT             NOT NULL CHECK (action IN ('fix_isrc', 'reacquire')),
    old_isrc                     TEXT,
    new_isrc                     TEXT,
    old_duration_seconds         DOUBLE PRECISION,
    old_catalog_duration_seconds DOUBLE PRECISION,
    old_audio_length_mismatch    BOOLEAN          NOT NULL,
    new_catalog_duration_seconds DOUBLE PRECISION NOT NULL,
    audio_version_at_apply       TEXT,
    applied_at                   TIMESTAMPTZ      NOT NULL DEFAULT now(),
    undone_at                    TIMESTAMPTZ,
    PRIMARY KEY (run_id, track_id)
);

CREATE INDEX IF NOT EXISTS idx_track_length_fixes_track ON track_length_fixes (track_id);
