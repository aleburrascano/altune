CREATE TABLE IF NOT EXISTS discovery_favorites (
    user_id     UUID NOT NULL,
    kind        TEXT NOT NULL,
    entity_key  TEXT NOT NULL,
    title       TEXT NOT NULL,
    subtitle    TEXT NOT NULL DEFAULT '',
    image_url   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, kind, entity_key)
);

CREATE INDEX IF NOT EXISTS idx_discovery_favorites_user_created
    ON discovery_favorites (user_id, created_at DESC);
