-- Client-supplied idempotency key for track creation. Lets two genuinely
-- concurrent POSTs — or a retry after a dropped response — that carry the same
-- key collapse to a single library row instead of creating duplicates. Nullable
-- so pre-existing rows and clients that omit the key are unaffected; the partial
-- unique index only constrains rows that actually carry a key.
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_tracks_user_idempotency_key
    ON tracks (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
