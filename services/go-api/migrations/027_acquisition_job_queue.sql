ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_job_kind TEXT NOT NULL DEFAULT 'acquire' CHECK (acquisition_job_kind IN ('acquire', 'replace'));
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_available_at TIMESTAMPTZ;
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_lease_until TIMESTAMPTZ;
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_attempts INTEGER NOT NULL DEFAULT 0;

UPDATE tracks SET acquisition_available_at = now()
    WHERE acquisition_status = 'pending' AND acquisition_available_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tracks_acquisition_available_at
    ON tracks (acquisition_available_at)
    WHERE acquisition_status = 'pending';
