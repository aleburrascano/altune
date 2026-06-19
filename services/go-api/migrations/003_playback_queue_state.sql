CREATE TABLE IF NOT EXISTS playback_queue_state (
    user_id      UUID PRIMARY KEY,
    track_ids    TEXT[] NOT NULL DEFAULT '{}',
    current_idx  INTEGER NOT NULL DEFAULT 0,
    position_ms  BIGINT NOT NULL DEFAULT 0,
    shuffled     BOOLEAN NOT NULL DEFAULT FALSE,
    repeat_mode  TEXT NOT NULL DEFAULT 'off',
    source_id    TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
