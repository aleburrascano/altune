package main

import (
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	errEmptyDatabaseURL = errors.New("DATABASE_URL is not set")
	errUnknownStatus    = errors.New("track_identity has an unknown status")
)

func openReadOnlyPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errEmptyDatabaseURL
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

const readyTracksSQL = `SELECT t.id::text, t.user_id::text, t.artist, t.title, COALESCE(t.audio_ref, ''),
	COALESCE(i.status, ''), COALESCE(i.isrc, ''), COALESCE(i.mb_recording_id, ''),
	COALESCE(i.deezer_track_id, ''), COALESCE(i.apple_song_id, ''), COALESCE(i.reason, '')
	FROM tracks t
	LEFT JOIN track_identity i ON i.track_id = t.id
	WHERE t.acquisition_status = 'ready'
	  AND ($1 = '' OR t.user_id::text = $1)
	  AND (COALESCE(cardinality($3::text[]), 0) = 0 OR t.id::text = ANY($3))
	ORDER BY t.user_id, t.artist, t.title, t.id
	LIMIT NULLIF($2, 0)`

func loadReadyTracks(ctx context.Context, pool *pgxpool.Pool, userID string, limit int, trackIDs []string) ([]libraryTrack, error) {
	var tracks []libraryTrack
	err := database.ReadOnly(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, readyTracksSQL, userID, limit, trackIDs)
		if err != nil {
			return fmt.Errorf("query ready tracks: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			track, err := scanTrack(rows)
			if err != nil {
				return err
			}
			tracks = append(tracks, track)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return tracks, nil
}

func scanTrack(rows pgx.Rows) (libraryTrack, error) {
	var track libraryTrack
	var status string
	identity := &track.Identity
	if err := rows.Scan(&track.ID, &track.UserID, &track.Artist, &track.Title, &track.AudioRef,
		&status, &identity.ISRC, &identity.MBRecordingID, &identity.DeezerTrackID, &identity.AppleSongID, &identity.Reason); err != nil {
		return libraryTrack{}, fmt.Errorf("scan ready track: %w", err)
	}
	parsed, err := parseStatus(status)
	if err != nil {
		return libraryTrack{}, fmt.Errorf("track %s: %w", track.ID, err)
	}
	identity.Status = parsed
	return track, nil
}

func parseStatus(raw string) (identityStatus, error) {
	switch status := identityStatus(raw); status {
	case statusBound, statusUnofficial, statusUnbound:
		return status, nil
	case "":
		return statusNone, nil
	default:
		return "", fmt.Errorf("%w: %q", errUnknownStatus, raw)
	}
}
