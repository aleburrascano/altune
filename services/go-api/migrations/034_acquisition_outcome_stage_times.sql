ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS stage_ms JSONB;
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS source_host TEXT NOT NULL DEFAULT '';
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS source_format_id TEXT NOT NULL DEFAULT '';
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS source_codec TEXT NOT NULL DEFAULT '';
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS source_kbps INTEGER CHECK (source_kbps > 0);
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS stored_bytes BIGINT CHECK (stored_bytes > 0);
ALTER TABLE acquisition_outcomes ADD COLUMN IF NOT EXISTS stored_codec TEXT NOT NULL DEFAULT '';
