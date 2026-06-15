-- Baseline schema for Altune.
-- Extracted from the running local Postgres on 2026-06-15.
-- Apply to Supabase via SQL Editor or psql.
--
-- NOTE: When you have Docker running, dump the real schema instead:
--   docker exec altune-postgres-dev pg_dump -U altune -d altune --schema-only --no-owner --no-privileges
-- This file is a best-effort reconstruction from the Go persistence layer.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- catalog: tracks
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

-- catalog: playlists
CREATE TABLE IF NOT EXISTS playlists (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id    UUID NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- catalog: playlist ↔ track membership
CREATE TABLE IF NOT EXISTS playlist_tracks (
    playlist_id UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    track_id    UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    PRIMARY KEY (playlist_id, track_id)
);

-- discovery: search history (ring buffer per user)
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

-- discovery: click tracking (sliding-window dedup)
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
