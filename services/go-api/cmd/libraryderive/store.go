package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var errEmptyDatabaseURL = errors.New("LIBRARYDERIVE_DATABASE_URL is not set")

func openReadOnlyPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errEmptyDatabaseURL
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 2
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
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

const boundTracksSQL = `SELECT t.id::text, t.user_id::text, t.title, t.artist, COALESCE(t.album, ''), COALESCE(t.album_artist, ''),
	COALESCE(t.isrc, ''), COALESCE(t.artwork_url, ''), COALESCE(t.year, 0), COALESCE(t.genre, ''), COALESCE(t.track_number, 0),
	COALESCE(t.catalog_duration_seconds, 0), COALESCE(t.duration_seconds, 0), t.version,
	COALESCE(i.isrc, ''), COALESCE(i.mb_recording_id, ''), (i.deezer_track_id IS NOT NULL OR i.apple_song_id IS NOT NULL)
	FROM track_identity i
	JOIN tracks t ON t.id = i.track_id
	WHERE i.status = 'bound'
	  AND t.acquisition_status = 'ready'
	  AND ($1 = '' OR t.user_id::text = $1)
	ORDER BY t.user_id, t.artist, t.album, t.title
	LIMIT NULLIF($2, 0)`

func loadBoundTracks(ctx context.Context, pool *pgxpool.Pool, userID string, limit int) ([]boundTrack, error) {
	rows, err := pool.Query(ctx, boundTracksSQL, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("query bound tracks: %w", err)
	}
	defer rows.Close()
	var tracks []boundTrack
	for rows.Next() {
		var t boundTrack
		if err := rows.Scan(&t.ID, &t.UserID, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.ISRC, &t.ArtworkURL, &t.Year,
			&t.Genre, &t.TrackNumber, &t.CatalogDurationSeconds, &t.FileSeconds, &t.Version, &t.IdentityISRC, &t.MBRecordingID, &t.CatalogConfirmed); err != nil {
			return nil, fmt.Errorf("scan bound track: %w", err)
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}
