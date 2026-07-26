ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_provenance TEXT;

CREATE INDEX IF NOT EXISTS idx_tracks_acquisition_provenance
    ON tracks (user_id, acquisition_provenance)
    WHERE acquisition_provenance IS NOT NULL;
