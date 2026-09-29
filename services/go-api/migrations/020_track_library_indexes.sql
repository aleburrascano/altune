
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_user_added_at
    ON tracks (user_id, added_at DESC, id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_user_lower_title
    ON tracks (user_id, lower(title), id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_user_year
    ON tracks (user_id, year DESC NULLS LAST, id DESC);

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_title_trgm
    ON tracks USING gin (title gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_artist_trgm
    ON tracks USING gin (artist gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_album_trgm
    ON tracks USING gin (album gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_album_artist_trgm
    ON tracks USING gin (album_artist gin_trgm_ops);
