CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS tracks (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id            UUID NOT NULL,
    title              TEXT NOT NULL,
    artist             TEXT NOT NULL,
    album              TEXT,
    duration_seconds   DOUBLE PRECISION,
    added_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    artwork_url        TEXT,
    acquisition_status TEXT NOT NULL DEFAULT 'pending',
    dedup_key          TEXT NOT NULL,
    year               INTEGER,
    genre              TEXT,
    track_number       INTEGER,
    album_artist       TEXT,
    isrc               TEXT,
    audio_ref          TEXT,
    failure_reason     TEXT,
    UNIQUE (user_id, dedup_key)
);

CREATE TABLE IF NOT EXISTS playlists (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id    UUID NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS playlist_tracks (
    playlist_id UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    track_id    UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    PRIMARY KEY (playlist_id, track_id)
);

CREATE TABLE IF NOT EXISTS discovery_search_history (
    id                       UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id                  UUID NOT NULL,
    query                    TEXT NOT NULL,
    query_norm               TEXT NOT NULL,
    executed_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    result_clicked_signature TEXT
);

CREATE INDEX IF NOT EXISTS idx_search_history_user_executed
    ON discovery_search_history (user_id, executed_at DESC);

CREATE TABLE IF NOT EXISTS discovery_search_clicks (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id          UUID NOT NULL,
    query_norm       TEXT NOT NULL,
    result_signature TEXT NOT NULL,
    position         INTEGER NOT NULL,
    confidence       TEXT NOT NULL,
    clicked_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_search_clicks_dedup
    ON discovery_search_clicks (user_id, query_norm, result_signature, clicked_at DESC);
