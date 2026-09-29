CREATE UNIQUE INDEX IF NOT EXISTS uq_discovery_events_user_event_id
    ON discovery_events (user_id, event_id)
    WHERE event_id IS NOT NULL;

DROP INDEX IF EXISTS uq_discovery_events_event_id;
