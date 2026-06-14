-- name: AddTrack :one
INSERT INTO tracks (
    id, user_id, title, artist, album, duration_seconds,
    added_at, artwork_url, acquisition_status, dedup_key,
    year, genre, track_number, album_artist, isrc, audio_ref, failure_reason
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17
)
ON CONFLICT (user_id, dedup_key) DO NOTHING
RETURNING id;

-- name: GetTrackByID :one
SELECT id, user_id, title, artist, album, duration_seconds,
       added_at, artwork_url, acquisition_status, dedup_key,
       year, genre, track_number, album_artist, isrc, audio_ref, failure_reason
FROM tracks
WHERE id = $1 AND user_id = $2;

-- name: GetTrackByDedupKey :one
SELECT id, user_id, title, artist, album, duration_seconds,
       added_at, artwork_url, acquisition_status, dedup_key,
       year, genre, track_number, album_artist, isrc, audio_ref, failure_reason
FROM tracks
WHERE user_id = $1 AND dedup_key = $2;

-- name: ListTracksForUser :many
SELECT id, user_id, title, artist, album, duration_seconds,
       added_at, artwork_url, acquisition_status, dedup_key,
       year, genre, track_number, album_artist, isrc, audio_ref, failure_reason
FROM tracks
WHERE user_id = $1
ORDER BY added_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: CountTracksForUser :one
SELECT count(*) FROM tracks WHERE user_id = $1;

-- name: UpdateTrack :exec
UPDATE tracks SET
    title = $3,
    artist = $4,
    album = $5,
    duration_seconds = $6,
    artwork_url = $7,
    acquisition_status = $8,
    dedup_key = $9,
    year = $10,
    genre = $11,
    track_number = $12,
    album_artist = $13,
    isrc = $14,
    audio_ref = $15,
    failure_reason = $16
WHERE id = $1 AND user_id = $2;

-- name: DeleteTrack :execrows
DELETE FROM tracks WHERE id = $1 AND user_id = $2;
