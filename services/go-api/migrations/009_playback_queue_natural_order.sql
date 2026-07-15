-- playback: persist the queue's natural (unshuffled) order alongside the play order.
--
-- track_ids already stores the queue in PLAY order (so track_ids[current_idx] is
-- the current track). natural_order stores the same ids in their original,
-- pre-shuffle order (album/playlist/library order). With both, resume can rebuild
-- the exact shuffled sequence AND let the user un-shuffle back to the original
-- order after relaunch. Purely a client-side reconstruction aid — the server does
-- no logic on it, it round-trips as an opaque snapshot like track_ids/source_id.
--
-- Nullable-by-default via DEFAULT '{}': existing rows (and non-fidelity clients)
-- simply carry an empty natural_order, and the client falls back to treating
-- track_ids as the queue (its prior behavior).
ALTER TABLE playback_queue_state
    ADD COLUMN IF NOT EXISTS natural_order TEXT[] NOT NULL DEFAULT '{}';
