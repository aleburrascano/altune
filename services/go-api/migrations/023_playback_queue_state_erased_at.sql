-- Marks a queue-state row as erased instead of deleting it, so a save that was
-- handled before the erasure cannot recreate it (#1594).
--
-- A deleted row left nothing for the write path to order against: the erased
-- row and a queue that was never saved looked identical, so a save delayed past
-- the erasure (waiting on a pooled connection, retried, held up on the wire)
-- found no conflict, ran as a plain insert, and silently restored the track
-- list, natural order and free-text source_id the user asked to be forgotten.
-- Keeping the row is what lets the existing updated_at ordering decide the
-- erasure too — and it is the only thing that decides it under concurrency,
-- because a save blocked on the erasure's row lock re-checks its guard against
-- the committed row, while a deleted row leaves it nothing to re-check.
--
-- The row an erasure leaves behind holds no queue data: DeleteForUser blanks
-- every PII column and stamps erased_at. It is read by nothing (GetForUser and
-- the deleted-identity sweep both skip it) and DeleteForUser reaps it once no
-- save older than it can still be in flight, so a deleted account's identifier
-- does not outlive the erasure that named it.
ALTER TABLE playback_queue_state
    ADD COLUMN IF NOT EXISTS erased_at TIMESTAMPTZ;

-- Partial, so it indexes only the erased rows the reap scans over, not the
-- stored queue of every user.
CREATE INDEX IF NOT EXISTS idx_playback_queue_state_erased_at
    ON playback_queue_state (erased_at) WHERE erased_at IS NOT NULL;
