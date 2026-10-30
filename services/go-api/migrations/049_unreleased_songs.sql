CREATE TABLE IF NOT EXISTS unreleased_artists (
    provider          TEXT NOT NULL,
    external_id       TEXT NOT NULL,
    name              TEXT NOT NULL,
    known_ids         JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempted_at      TIMESTAMPTZ,
    built_at          TIMESTAMPTZ,
    complete          BOOLEAN NOT NULL DEFAULT false,
    song_count        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (provider, external_id)
);

CREATE TABLE IF NOT EXISTS unreleased_songs (
    provider    TEXT NOT NULL,
    external_id TEXT NOT NULL,
    position    INTEGER NOT NULL,
    title       TEXT NOT NULL,
    song        JSONB NOT NULL,
    PRIMARY KEY (provider, external_id, position),
    FOREIGN KEY (provider, external_id)
        REFERENCES unreleased_artists (provider, external_id) ON DELETE CASCADE
);
