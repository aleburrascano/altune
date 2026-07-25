ALTER TABLE playback_queue_state
    ADD COLUMN IF NOT EXISTS natural_order TEXT[] NOT NULL DEFAULT '{}';
