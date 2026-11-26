SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS acquisition_jobs (
    track_id       UUID PRIMARY KEY REFERENCES tracks (id) ON DELETE CASCADE,
    job_kind       TEXT NOT NULL DEFAULT 'acquire' CHECK (job_kind IN ('acquire', 'replace', 'upgrade')),
    available_at   TIMESTAMPTZ,
    lease_until    TIMESTAMPTZ,
    attempts       INTEGER NOT NULL DEFAULT 0,
    run_attempts   INTEGER NOT NULL DEFAULT 0,
    correlation_id TEXT,
    started_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_acquisition_jobs_available_at
    ON acquisition_jobs (available_at)
    WHERE available_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_acquisition_jobs_lease_until
    ON acquisition_jobs (lease_until)
    WHERE lease_until IS NOT NULL;

CREATE TABLE IF NOT EXISTS acquisition_upgrade_state (
    track_id             UUID PRIMARY KEY REFERENCES tracks (id) ON DELETE CASCADE,
    evidence             JSONB NOT NULL DEFAULT '{}'::jsonb,
    rejected_source_keys TEXT[] NOT NULL DEFAULT '{}'
);

ALTER TABLE acquisition_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE acquisition_upgrade_state ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'altune_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON acquisition_jobs, acquisition_upgrade_state TO altune_app;
    END IF;
END $$;

CREATE OR REPLACE FUNCTION mirror_acquisition_job() RETURNS trigger AS $$
BEGIN
    INSERT INTO acquisition_jobs (track_id, job_kind, available_at, lease_until, attempts, run_attempts, correlation_id, started_at)
    VALUES (NEW.id, NEW.acquisition_job_kind, NEW.acquisition_available_at, NEW.acquisition_lease_until,
            NEW.acquisition_attempts, NEW.acquisition_run_attempts, NEW.acquisition_correlation_id, NEW.acquisition_started_at)
    ON CONFLICT (track_id) DO UPDATE SET
        job_kind = EXCLUDED.job_kind,
        available_at = EXCLUDED.available_at,
        lease_until = EXCLUDED.lease_until,
        attempts = EXCLUDED.attempts,
        run_attempts = EXCLUDED.run_attempts,
        correlation_id = EXCLUDED.correlation_id,
        started_at = EXCLUDED.started_at;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION mirror_acquisition_upgrade_state() RETURNS trigger AS $$
BEGIN
    INSERT INTO acquisition_upgrade_state (track_id, evidence, rejected_source_keys)
    VALUES (
        NEW.id,
        COALESCE((
            SELECT jsonb_object_agg(key, value)
            FROM jsonb_each(COALESCE(NEW.acquisition_evidence, '{}'::jsonb))
            WHERE key = ANY (ARRAY[
                'upgrade_tried_at', 'upgrade_tries',
                'best_available_generation', 'best_available_reason', 'best_available_at',
                'in_place_generation', 'in_place_at', 'in_place_basis', 'in_place_bit_error_rate', 'in_place_failures'
            ])
        ), '{}'::jsonb),
        COALESCE(NEW.rejected_source_keys, '{}')
    )
    ON CONFLICT (track_id) DO UPDATE SET
        evidence = EXCLUDED.evidence,
        rejected_source_keys = EXCLUDED.rejected_source_keys;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tracks_mirror_acquisition_job ON tracks;
CREATE TRIGGER tracks_mirror_acquisition_job
    AFTER INSERT OR UPDATE OF acquisition_job_kind, acquisition_available_at, acquisition_lease_until,
        acquisition_attempts, acquisition_run_attempts, acquisition_correlation_id, acquisition_started_at
    ON tracks
    FOR EACH ROW EXECUTE FUNCTION mirror_acquisition_job();

DROP TRIGGER IF EXISTS tracks_mirror_acquisition_upgrade_state ON tracks;
CREATE TRIGGER tracks_mirror_acquisition_upgrade_state
    AFTER INSERT OR UPDATE OF acquisition_evidence, rejected_source_keys
    ON tracks
    FOR EACH ROW EXECUTE FUNCTION mirror_acquisition_upgrade_state();

INSERT INTO acquisition_jobs (track_id, job_kind, available_at, lease_until, attempts, run_attempts, correlation_id, started_at)
SELECT id, acquisition_job_kind, acquisition_available_at, acquisition_lease_until,
       acquisition_attempts, acquisition_run_attempts, acquisition_correlation_id, acquisition_started_at
FROM tracks
ON CONFLICT (track_id) DO NOTHING;

INSERT INTO acquisition_upgrade_state (track_id, evidence, rejected_source_keys)
SELECT t.id,
       COALESCE((
           SELECT jsonb_object_agg(key, value)
           FROM jsonb_each(COALESCE(t.acquisition_evidence, '{}'::jsonb))
           WHERE key = ANY (ARRAY[
               'upgrade_tried_at', 'upgrade_tries',
               'best_available_generation', 'best_available_reason', 'best_available_at',
               'in_place_generation', 'in_place_at', 'in_place_basis', 'in_place_bit_error_rate', 'in_place_failures'
           ])
       ), '{}'::jsonb),
       COALESCE(t.rejected_source_keys, '{}')
FROM tracks t
ON CONFLICT (track_id) DO NOTHING;
