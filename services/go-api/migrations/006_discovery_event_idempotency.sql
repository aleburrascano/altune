-- discovery: two-tier reliability for the label-critical events.
-- library_add / wrong_album are routed through a client outbox with an
-- idempotency key (event_id) and retried on reconnect; a lost one is a lost
-- label AND a library-state bug, so they must arrive at-least-once. Dedup on
-- insert via event_id makes the retry safe. Dual timestamps separate when the
-- client recorded it (client_occurred_at) from when the server received it
-- (received_at) — the gap measures outbox lag / offline buffering.
ALTER TABLE discovery_events ADD COLUMN IF NOT EXISTS event_id UUID;
ALTER TABLE discovery_events ADD COLUMN IF NOT EXISTS client_occurred_at TIMESTAMPTZ;
ALTER TABLE discovery_events ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Dedup key: only the critical tier carries an event_id (NULLs don't collide in
-- a unique index), so fire-and-forget events are unaffected. ON CONFLICT targets
-- this partial index to make a retried critical event a no-op.
CREATE UNIQUE INDEX IF NOT EXISTS uq_discovery_events_event_id
    ON discovery_events (event_id)
    WHERE event_id IS NOT NULL;
