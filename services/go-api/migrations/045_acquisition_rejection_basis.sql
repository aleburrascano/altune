ALTER TABLE acquisition_rejections ADD COLUMN IF NOT EXISTS expected_seconds DOUBLE PRECISION;
ALTER TABLE acquisition_rejections ADD COLUMN IF NOT EXISTS reference_mbid TEXT;
