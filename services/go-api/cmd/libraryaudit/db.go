package main

import (
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errEmptyDatabaseURL = errors.New("AUDIT_DATABASE_URL is not set")

func openPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errEmptyDatabaseURL
	}
	return database.NewPool(ctx, databaseURL, 2)
}

const readyTracksQuery = `SELECT id, user_id, title, artist, COALESCE(album, ''), COALESCE(album_artist, ''),
	COALESCE(isrc, ''), COALESCE(artwork_url, ''), COALESCE(year, 0), COALESCE(duration_seconds, 0), version,
	COALESCE(genre, ''), COALESCE(track_number, 0), COALESCE(catalog_duration_seconds, 0)
	FROM tracks
	WHERE acquisition_status = 'ready'
	  AND ($1 = '' OR user_id::text = $1)
	ORDER BY user_id, artist, album, title
	LIMIT NULLIF($2, 0)`

func loadReadyTracks(ctx context.Context, pool *pgxpool.Pool, userID string, limit int) ([]libraryTrack, error) {
	var tracks []libraryTrack
	err := database.ReadOnly(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, readyTracksQuery, userID, limit)
		if err != nil {
			return fmt.Errorf("query ready tracks: %w", err)
		}
		tracks, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (libraryTrack, error) {
			var t libraryTrack
			err := row.Scan(&t.ID, &t.UserID, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist,
				&t.ISRC, &t.ArtworkURL, &t.Year, &t.DurationSeconds, &t.Version,
				&t.Genre, &t.TrackNumber, &t.CatalogDurationSeconds)
			return t, err
		})
		if err != nil {
			return fmt.Errorf("scan ready track: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tracks, nil
}
