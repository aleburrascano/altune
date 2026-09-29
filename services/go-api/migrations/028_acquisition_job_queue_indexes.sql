
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_user_acquisition_lease_until
    ON tracks (user_id, acquisition_lease_until)
    WHERE acquisition_status = 'pending';
