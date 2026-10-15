ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS job_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS run INTEGER CHECK (run > 0);
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS provenance TEXT NOT NULL DEFAULT '';
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION;
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS source_key TEXT NOT NULL DEFAULT '';
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS target_seconds DOUBLE PRECISION CHECK (target_seconds > 0);
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS measured_seconds DOUBLE PRECISION CHECK (measured_seconds > 0);
