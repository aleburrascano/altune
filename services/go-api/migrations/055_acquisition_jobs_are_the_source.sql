SET LOCAL lock_timeout = '5s';

CREATE OR REPLACE FUNCTION mirror_acquisition_job() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND EXISTS (
        SELECT 1 FROM acquisition_jobs j
        WHERE j.track_id = NEW.id
            AND (j.job_kind, j.available_at, j.lease_until, j.attempts, j.run_attempts, j.correlation_id, j.started_at)
                IS NOT DISTINCT FROM
                (NEW.acquisition_job_kind, NEW.acquisition_available_at, NEW.acquisition_lease_until,
                 NEW.acquisition_attempts, NEW.acquisition_run_attempts, NEW.acquisition_correlation_id, NEW.acquisition_started_at)
    ) THEN
        RETURN NULL;
    END IF;
    INSERT INTO acquisition_jobs (track_id, job_kind, available_at, lease_until, attempts, run_attempts, correlation_id, started_at)
    VALUES (NEW.id, NEW.acquisition_job_kind,
            CASE WHEN TG_OP = 'INSERT'
                 THEN COALESCE(NEW.acquisition_available_at, CASE WHEN NEW.acquisition_status = 'pending' THEN now() END)
                 ELSE NEW.acquisition_available_at END,
            NEW.acquisition_lease_until, NEW.acquisition_attempts, NEW.acquisition_run_attempts,
            NEW.acquisition_correlation_id, NEW.acquisition_started_at)
    ON CONFLICT (track_id) DO UPDATE SET
        job_kind = EXCLUDED.job_kind,
        available_at = EXCLUDED.available_at,
        lease_until = EXCLUDED.lease_until,
        attempts = EXCLUDED.attempts,
        run_attempts = EXCLUDED.run_attempts,
        correlation_id = EXCLUDED.correlation_id,
        started_at = EXCLUDED.started_at
    WHERE (acquisition_jobs.job_kind, acquisition_jobs.available_at, acquisition_jobs.lease_until,
           acquisition_jobs.attempts, acquisition_jobs.run_attempts, acquisition_jobs.correlation_id,
           acquisition_jobs.started_at)
        IS DISTINCT FROM
          (EXCLUDED.job_kind, EXCLUDED.available_at, EXCLUDED.lease_until,
           EXCLUDED.attempts, EXCLUDED.run_attempts, EXCLUDED.correlation_id, EXCLUDED.started_at);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION mirror_acquisition_job_back() RETURNS trigger AS $$
BEGIN
    UPDATE tracks SET
        acquisition_job_kind = NEW.job_kind,
        acquisition_available_at = NEW.available_at,
        acquisition_lease_until = NEW.lease_until,
        acquisition_attempts = NEW.attempts,
        acquisition_run_attempts = NEW.run_attempts,
        acquisition_correlation_id = NEW.correlation_id
    WHERE id = NEW.track_id
        AND (acquisition_job_kind, acquisition_available_at, acquisition_lease_until,
             acquisition_attempts, acquisition_run_attempts, acquisition_correlation_id)
        IS DISTINCT FROM
            (NEW.job_kind, NEW.available_at, NEW.lease_until,
             NEW.attempts, NEW.run_attempts, NEW.correlation_id);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS acquisition_jobs_mirror_back ON acquisition_jobs;
CREATE TRIGGER acquisition_jobs_mirror_back
    AFTER INSERT OR UPDATE ON acquisition_jobs
    FOR EACH ROW EXECUTE FUNCTION mirror_acquisition_job_back();
