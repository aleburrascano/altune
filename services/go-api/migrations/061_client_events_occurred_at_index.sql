CREATE INDEX IF NOT EXISTS idx_client_events_occurred_at
    ON client_events (occurred_at);
