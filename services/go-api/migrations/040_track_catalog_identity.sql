ALTER TABLE tracks
    ADD COLUMN IF NOT EXISTS catalog_mbid     TEXT,
    ADD COLUMN IF NOT EXISTS catalog_explicit BOOLEAN,
    ADD COLUMN IF NOT EXISTS catalog_sources  JSONB NOT NULL DEFAULT '[]';
