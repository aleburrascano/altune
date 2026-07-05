-- Featured artists: guest ("feat.") credits on a track, sourced from the
-- discovery resolver (MusicBrainz artist-credit + Deezer contributors).
-- Normalized so "everything featuring X" is an index-clean join. See ADR-0019
-- and docs/specs/featured-artists/spec.md.

-- Canonical featured artist, one row per user per identity. identity_key is the
-- stable grouping key: MBID, else the Deezer id, else the normalized name.
CREATE TABLE IF NOT EXISTS featured_artists (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id      UUID NOT NULL,
    mbid         TEXT,
    deezer_id    BIGINT,
    name         TEXT NOT NULL,
    norm_name    TEXT NOT NULL,
    identity_key TEXT GENERATED ALWAYS AS (
        COALESCE(mbid, 'dz:' || deezer_id::text, 'name:' || norm_name)
    ) STORED,
    UNIQUE (user_id, identity_key)
);

-- Ordered membership: which featured artists are on which track.
CREATE TABLE IF NOT EXISTS track_featured_artists (
    track_id           UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    featured_artist_id UUID NOT NULL REFERENCES featured_artists(id) ON DELETE CASCADE,
    position           INTEGER NOT NULL,
    PRIMARY KEY (track_id, featured_artist_id)
);

-- "Everything featuring X": look up all tracks by featured_artist_id.
CREATE INDEX IF NOT EXISTS idx_tfa_featured_artist ON track_featured_artists(featured_artist_id);
