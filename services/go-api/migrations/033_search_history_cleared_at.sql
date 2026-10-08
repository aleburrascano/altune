CREATE TABLE IF NOT EXISTS discovery_search_history_cleared (
    user_id    UUID        PRIMARY KEY,
    cleared_at TIMESTAMPTZ NOT NULL
);
