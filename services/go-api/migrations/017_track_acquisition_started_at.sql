ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_started_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_tracks_stale_pending
    ON tracks (acquisition_started_at)
    WHERE acquisition_status = 'pending' AND acquisition_started_at IS NOT NULL;
