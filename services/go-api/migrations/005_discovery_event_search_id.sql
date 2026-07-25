ALTER TABLE discovery_events ADD COLUMN IF NOT EXISTS search_id UUID;

CREATE INDEX IF NOT EXISTS idx_discovery_events_search_id
    ON discovery_events (search_id)
    WHERE search_id IS NOT NULL;
