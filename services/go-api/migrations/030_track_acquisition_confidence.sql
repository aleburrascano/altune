ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_confidence REAL;
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS acquisition_evidence JSONB;
