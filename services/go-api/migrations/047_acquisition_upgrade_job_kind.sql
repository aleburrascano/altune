ALTER TABLE tracks DROP CONSTRAINT IF EXISTS tracks_acquisition_job_kind_check;
ALTER TABLE tracks ADD CONSTRAINT tracks_acquisition_job_kind_check
    CHECK (acquisition_job_kind IN ('acquire', 'replace', 'upgrade'));

ALTER TABLE acquisition_outcomes DROP CONSTRAINT IF EXISTS acquisition_outcomes_outcome_check;
ALTER TABLE acquisition_outcomes ADD CONSTRAINT acquisition_outcomes_outcome_check
    CHECK (outcome IN ('succeeded', 'failed', 'cancelled', 'kept'));
