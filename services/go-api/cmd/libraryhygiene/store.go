package main

import (
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errEmptyDatabaseURL = errors.New(databaseURLVarName + " is not set")

const unmatchedTracksSQL = `SELECT t.id::text, t.user_id::text, t.title, t.artist, COALESCE(t.album, ''), COALESCE(t.artwork_url, ''), COALESCE(t.audio_source_url, ''), t.version
	FROM track_identity i
	JOIN tracks t ON t.id = i.track_id
	WHERE i.status IN ('unbound', 'unofficial')
	  AND t.acquisition_status = 'ready'
	ORDER BY t.user_id, t.artist, t.title, t.id`

func openPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errEmptyDatabaseURL
	}
	return database.NewPool(ctx, databaseURL, 1)
}

func loadUnmatchedTracks(ctx context.Context, pool *pgxpool.Pool) ([]unmatchedTrack, error) {
	var tracks []unmatchedTrack
	err := database.ReadOnly(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, unmatchedTracksSQL)
		if err != nil {
			return fmt.Errorf("query unmatched tracks: %w", err)
		}
		tracks, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (unmatchedTrack, error) {
			var t unmatchedTrack
			err := row.Scan(&t.ID, &t.UserID, &t.Title, &t.Artist, &t.Album, &t.ArtworkURL, &t.SourceURL, &t.Version)
			return t, err
		})
		if err != nil {
			return fmt.Errorf("scan unmatched track: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tracks, nil
}
