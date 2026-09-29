
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

ALTER TABLE playlist_tracks
    ADD CONSTRAINT playlist_tracks_playlist_position_key
    UNIQUE (playlist_id, position) DEFERRABLE INITIALLY DEFERRED;
