ALTER TABLE unreleased_songs ADD COLUMN IF NOT EXISTS search_key TEXT;

CREATE INDEX IF NOT EXISTS idx_unreleased_songs_search_key_trgm
    ON unreleased_songs USING gin (search_key gin_trgm_ops);
