-- Durable queue of audio objects left in storage after their track row was
-- deleted but the storage delete failed (#1058). The orphaned audio reconcile
-- job retries each delete on an interval and removes the row once the object is
-- gone, or once a track references the key again (it is no longer an orphan).
--
-- Safe to deploy before this runs: without the table the delete path falls back
-- to the tagged log line + metric and the sweep idles (SQLSTATE 42P01).
--
-- One row per storage key: keys can be shared by tracks with equivalent
-- metadata, so re-orphaning the same key refreshes the existing row. No FK to
-- tracks: the track row is already gone when the orphan is recorded.
CREATE TABLE IF NOT EXISTS orphaned_audio (
    audio_ref       TEXT        PRIMARY KEY,
    user_id         UUID        NOT NULL,
    track_id        UUID        NOT NULL,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts        INTEGER     NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    last_error      TEXT
);

-- The sweep's reference check looks tracks up by audio_ref. The orphaned_audio
-- table is small, but this lookup would otherwise scan every track per orphan.
--
-- APPLY WITH PLAIN `psql -f` (autocommit), as for 020: CREATE INDEX
-- CONCURRENTLY cannot run inside a transaction block. If the build fails
-- midway, drop the INVALID index (see 020) before re-running this file.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_audio_ref
    ON tracks (audio_ref)
    WHERE audio_ref IS NOT NULL;
