ALTER TABLE tracks ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_tracks_user_idempotency_key
    ON tracks (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
