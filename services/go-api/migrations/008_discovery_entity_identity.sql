-- discovery: durable entity-identity map (the reverse identity bridge)
-- Keyed by (provider, external_id) -> the MusicBrainz id + bridged cross-provider
-- ids the merge computes when MusicBrainz answers. Persisting it lets a later
-- search resolve an entity's identity (and thus its correct artwork) even when
-- MusicBrainz is absent from that fanOut (rate-limited / circuit-open). Keying on
-- stable provider ids, not fuzzy names, is what keeps same-name artists ("Che")
-- from inheriting each other's identity. Source of truth; Redis fronts it as a
-- read-through cache. One row per (provider, external_id) ever bridged — small.
CREATE TABLE IF NOT EXISTS entity_identity (
    provider    TEXT NOT NULL,
    external_id TEXT NOT NULL,
    kind        TEXT NOT NULL,
    mbid        TEXT NOT NULL,
    xref        JSONB NOT NULL DEFAULT '{}'::jsonb,
    resolved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- kind is in the key: provider id spaces can numerically overlap across kinds
    -- (a Deezer artist id and album id could coincide), and the lookup always
    -- knows the kind it's resolving.
    PRIMARY KEY (provider, external_id, kind)
);

-- Reverse lookups (all provider ids bridged to one MBID) for future backfill.
CREATE INDEX IF NOT EXISTS idx_entity_identity_mbid
    ON entity_identity (mbid);
