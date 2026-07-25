CREATE TABLE IF NOT EXISTS discovery_events (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID NOT NULL,
    event_type  TEXT NOT NULL,
    query_norm  TEXT,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_discovery_events_type_time
    ON discovery_events (event_type, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_discovery_events_user_time
    ON discovery_events (user_id, occurred_at DESC);
