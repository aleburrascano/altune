-- discovery: telemetry events (append-only interaction envelope)
-- Typed core columns + JSONB payload so the envelope can grow new event types
-- and fields without a schema migration ("collect richly, model lazily").
CREATE TABLE IF NOT EXISTS discovery_events (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID NOT NULL,
    event_type  TEXT NOT NULL,
    query_norm  TEXT,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Coverage signal A and the eval read by event type over time windows.
CREATE INDEX IF NOT EXISTS idx_discovery_events_type_time
    ON discovery_events (event_type, occurred_at DESC);

-- Per-user reads (provider-quality / behavioral signals).
CREATE INDEX IF NOT EXISTS idx_discovery_events_user_time
    ON discovery_events (user_id, occurred_at DESC);
