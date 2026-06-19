-- Indexes for cross-user relationship queries (entity-relationship search enrichment).

CREATE INDEX IF NOT EXISTS idx_tracks_lower_album
    ON tracks (lower(album))
    WHERE album IS NOT NULL AND album != '';

CREATE INDEX IF NOT EXISTS idx_tracks_lower_artist
    ON tracks (lower(artist));
