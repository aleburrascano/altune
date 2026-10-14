package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type selection struct {
	runID uuid.UUID
	since time.Time
}

func (s selection) changedOnly() bool {
	return s.runID != uuid.Nil || !s.since.IsZero()
}

type trackStore struct {
	pool *pgxpool.Pool
}

const readyTracksSQL = `SELECT id::text, user_id::text, title, artist, COALESCE(album, ''), COALESCE(album_artist, ''),
	COALESCE(genre, ''), COALESCE(year, 0), COALESCE(track_number, 0), audio_ref
	FROM tracks
	WHERE acquisition_status = 'ready' AND COALESCE(audio_ref, '') <> ''`

const changedFilterSQL = `
	AND id IN (SELECT track_id FROM track_metadata_changes
		WHERE ($1::uuid IS NULL OR run_id = $1)
		AND ($2::timestamptz IS NULL OR applied_at >= $2 OR undone_at >= $2))`

func (s trackStore) readyTracks(ctx context.Context, sel selection) ([]trackRow, error) {
	query, args := readyTracksSQL+" ORDER BY id", []any(nil)
	if sel.changedOnly() {
		query, args = readyTracksSQL+changedFilterSQL+" ORDER BY id", selectionArgs(sel)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query ready tracks: %w", err)
	}
	defer rows.Close()
	var tracks []trackRow
	for rows.Next() {
		track, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, track)
	}
	return tracks, rows.Err()
}

func scanTrack(rows pgx.Rows) (trackRow, error) {
	var track trackRow
	tags := &track.Tags
	if err := rows.Scan(&track.ID, &track.UserID, &tags.Title, &tags.Artist, &tags.Album, &tags.AlbumArtist,
		&tags.Genre, &tags.Year, &tags.TrackNumber, &track.Ref); err != nil {
		return trackRow{}, fmt.Errorf("scan ready track: %w", err)
	}
	return track, nil
}

func selectionArgs(sel selection) []any {
	var runID, since any
	if sel.runID != uuid.Nil {
		runID = sel.runID
	}
	if !sel.since.IsZero() {
		since = sel.since
	}
	return []any{runID, since}
}

const stillServesSQL = `SELECT EXISTS (SELECT 1 FROM tracks
	WHERE id = $1 AND user_id = $2 AND audio_ref = $3 AND acquisition_status = 'ready')`

func (s trackStore) stillServes(ctx context.Context, track trackRow) (bool, error) {
	var serves bool
	err := s.pool.QueryRow(ctx, stillServesSQL, track.ID, track.UserID, track.Ref).Scan(&serves)
	return serves, err
}
