CREATE TABLE IF NOT EXISTS client_events (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id            UUID NOT NULL,
    event_type         TEXT NOT NULL,
    event_id           UUID,
    correlation_id     TEXT,
    client_occurred_at TIMESTAMPTZ,
    payload            JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at        TIMESTAMPTZ NOT NULL,
    received_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_client_events_user_event_id
    ON client_events (user_id, event_id)
    WHERE event_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_client_events_type_time
    ON client_events (event_type, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_client_events_user_time
    ON client_events (user_id, occurred_at DESC);
