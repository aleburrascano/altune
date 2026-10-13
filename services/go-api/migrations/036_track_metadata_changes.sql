CREATE TABLE IF NOT EXISTS track_metadata_changes (
    run_id     UUID        NOT NULL,
    track_id   UUID        NOT NULL,
    user_id    UUID        NOT NULL,
    field      TEXT        NOT NULL CHECK (field IN ('year', 'album_artist', 'isrc')),
    old_value  TEXT,
    new_value  TEXT,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    undone_at  TIMESTAMPTZ,
    PRIMARY KEY (run_id, track_id, field)
);
