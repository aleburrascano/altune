-- Turns the pending row itself into the durable acquisition job (#3175, part
-- of #3122). acquisition_job_kind distinguishes a first acquire from a
-- replace, acquisition_available_at is when the job is next claimable,
-- acquisition_lease_until is the exclusive claim a worker holds while it
-- runs, and acquisition_attempts counts claims for the retry/backoff policy.
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_job_kind TEXT NOT NULL DEFAULT 'acquire' CHECK (acquisition_job_kind IN ('acquire', 'replace'));
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_available_at TIMESTAMPTZ;
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_lease_until TIMESTAMPTZ;
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_attempts INTEGER NOT NULL DEFAULT 0;

-- Existing pending rows predate the queue and carry no available_at; without
-- this backfill Claim's `available_at <= now()` guard would starve them
-- forever, since nothing else ever sets the column on an already-pending row.
UPDATE tracks SET acquisition_available_at = now()
    WHERE acquisition_status = 'pending' AND acquisition_available_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tracks_acquisition_available_at
    ON tracks (acquisition_available_at)
    WHERE acquisition_status = 'pending';
