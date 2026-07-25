CREATE TABLE IF NOT EXISTS entity_identity (
    provider    TEXT NOT NULL,
    external_id TEXT NOT NULL,
    kind        TEXT NOT NULL,
    mbid        TEXT NOT NULL,
    xref        JSONB NOT NULL DEFAULT '{}'::jsonb,
    resolved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, external_id, kind)
);

CREATE INDEX IF NOT EXISTS idx_entity_identity_mbid
    ON entity_identity (mbid);
