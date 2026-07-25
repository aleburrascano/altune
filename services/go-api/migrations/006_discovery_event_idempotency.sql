ALTER TABLE discovery_events ADD COLUMN IF NOT EXISTS event_id UUID;
ALTER TABLE discovery_events ADD COLUMN IF NOT EXISTS client_occurred_at TIMESTAMPTZ;
ALTER TABLE discovery_events ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE UNIQUE INDEX IF NOT EXISTS uq_discovery_events_event_id
    ON discovery_events (event_id)
    WHERE event_id IS NOT NULL;
