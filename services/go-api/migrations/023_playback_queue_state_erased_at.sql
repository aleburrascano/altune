ALTER TABLE playback_queue_state
    ADD COLUMN IF NOT EXISTS erased_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_playback_queue_state_erased_at
    ON playback_queue_state (erased_at) WHERE erased_at IS NOT NULL;
