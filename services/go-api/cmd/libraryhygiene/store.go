package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var errEmptyDatabaseURL = errors.New(databaseURLVarName + " is not set")

const unmatchedTracksSQL = `SELECT t.id::text, t.user_id::text, t.title, t.artist, COALESCE(t.album, ''), COALESCE(t.artwork_url, ''), COALESCE(t.audio_source_url, ''), t.version
	FROM track_identity i
	JOIN tracks t ON t.id = i.track_id
	WHERE i.status IN ('unbound', 'unofficial')
	  AND t.acquisition_status = 'ready'
	ORDER BY t.user_id, t.artist, t.title, t.id`

func openReadOnlyPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errEmptyDatabaseURL
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 1
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

func loadUnmatchedTracks(ctx context.Context, pool *pgxpool.Pool) ([]unmatchedTrack, error) {
	rows, err := pool.Query(ctx, unmatchedTracksSQL)
	if err != nil {
		return nil, fmt.Errorf("query unmatched tracks: %w", err)
	}
	defer rows.Close()
	var tracks []unmatchedTrack
	for rows.Next() {
		var t unmatchedTrack
		if err := rows.Scan(&t.ID, &t.UserID, &t.Title, &t.Artist, &t.Album, &t.ArtworkURL, &t.SourceURL, &t.Version); err != nil {
			return nil, fmt.Errorf("scan unmatched track: %w", err)
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}
