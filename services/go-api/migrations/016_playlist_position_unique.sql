-- Guard playlist ordering at the schema level. Concurrent additions used to be
-- able to compute the same "next position" from a stale snapshot and both write
-- it, leaving two rows tied at one position and a gap at the next. A UNIQUE
-- (playlist_id, position) makes that corruption impossible to persist.

-- Heal any pre-existing duplicate/gapped positions (artifacts of the old race)
-- so the constraint can be added against already-dirty tables. Runs before the
-- constraint exists, so transient duplicates during the rewrite are harmless.
WITH renumbered AS (
    SELECT playlist_id, track_id,
           ROW_NUMBER() OVER (PARTITION BY playlist_id ORDER BY position, track_id) - 1 AS new_pos
    FROM playlist_tracks
)
UPDATE playlist_tracks pt
SET position = r.new_pos
FROM renumbered r
WHERE pt.playlist_id = r.playlist_id
  AND pt.track_id = r.track_id
  AND pt.position <> r.new_pos;

-- DEFERRABLE INITIALLY DEFERRED: the check runs at COMMIT, not per row. Reorder
-- and removal renumbering rewrite positions within a single transaction and pass
-- through transient duplicates mid-statement (e.g. swapping two rows); deferring
-- lets those land while still rejecting any transaction whose final state ties
-- two tracks at one position.
ALTER TABLE playlist_tracks
    ADD CONSTRAINT playlist_tracks_playlist_position_key
    UNIQUE (playlist_id, position) DEFERRABLE INITIALLY DEFERRED;
