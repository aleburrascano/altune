CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_acquisition_available_at_scheduled
    ON tracks (acquisition_available_at)
    WHERE acquisition_available_at IS NOT NULL;
