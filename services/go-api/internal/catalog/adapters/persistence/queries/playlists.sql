-- name: CreatePlaylist :exec
INSERT INTO playlists (id, user_id, name, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5);

-- name: ListPlaylistsForUser :many
SELECT id, user_id, name, created_at, updated_at
FROM playlists
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetPlaylistByID :one
SELECT id, user_id, name, created_at, updated_at
FROM playlists
WHERE id = $1 AND user_id = $2;

-- name: UpdatePlaylist :exec
UPDATE playlists SET name = $3, updated_at = $4
WHERE id = $1 AND user_id = $2;

-- name: DeletePlaylist :execrows
DELETE FROM playlists WHERE id = $1 AND user_id = $2;

-- name: AddTrackToPlaylist :exec
INSERT INTO playlist_tracks (playlist_id, track_id, position)
VALUES ($1, $2, $3);

-- name: RemoveTrackFromPlaylist :exec
DELETE FROM playlist_tracks
WHERE playlist_id = $1 AND track_id = $2;

-- name: ListPlaylistTracks :many
SELECT pt.track_id, pt.position,
       t.id, t.user_id, t.title, t.artist, t.album, t.duration_seconds,
       t.added_at, t.artwork_url, t.acquisition_status, t.dedup_key,
       t.year, t.genre, t.track_number, t.album_artist, t.isrc, t.audio_ref, t.failure_reason
FROM playlist_tracks pt
JOIN tracks t ON t.id = pt.track_id
WHERE pt.playlist_id = $1
ORDER BY pt.position ASC;

-- name: UpdatePlaylistTrackPositions :exec
UPDATE playlist_tracks SET position = $3
WHERE playlist_id = $1 AND track_id = $2;

-- name: GetPlaylistTrackCount :one
SELECT count(*) FROM playlist_tracks WHERE playlist_id = $1;

-- name: GetPreviewArtwork :many
SELECT t.artwork_url
FROM playlist_tracks pt
JOIN tracks t ON t.id = pt.track_id
WHERE pt.playlist_id = $1 AND t.artwork_url IS NOT NULL
GROUP BY t.artwork_url
ORDER BY MIN(pt.position) ASC
LIMIT 4;
