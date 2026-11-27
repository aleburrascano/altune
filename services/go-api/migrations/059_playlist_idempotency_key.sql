ALTER TABLE playlists ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_playlists_user_idempotency_key
    ON playlists (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
