package main

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedLibraryTrack(t *testing.T) (*pgxpool.Pool, string, string) {
	t.Helper()
	pool := sharedtest.Pool(t)
	id, user := uuid.New(), uuid.New()
	title, artist := "Title "+id.String(), "Artist "+id.String()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, dedup_key, acquisition_status) VALUES ($1, $2, $3, $4, gen_random_uuid()::text, 'pending')`,
		id, user, title, artist); err != nil {
		t.Fatalf("seed track: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, id) })
	return pool, title, artist
}

func TestLibraryLoaders_ReadTheStoredTracks(t *testing.T) {
	pool, title, artist := seedLibraryTrack(t)
	ctx := context.Background()

	entities, err := loadLibraryEntities(ctx, pool, 0, false)
	if err != nil || !slices.ContainsFunc(entities, func(e discovery.LibraryEntity) bool { return e.Title == title && e.Artist == artist }) {
		t.Fatalf("loadLibraryEntities = %d entities, %v; want the seeded track among them", len(entities), err)
	}
	terms, err := loadLibraryTerms(ctx, pool, 0)
	if err != nil || !slices.Contains(terms, title) || !slices.Contains(terms, artist) {
		t.Fatalf("loadLibraryTerms = %d terms, %v; want the seeded title and artist", len(terms), err)
	}
	artists, err := loadDistinctArtists(ctx, pool, 0)
	if err != nil || !slices.Contains(artists, artist) {
		t.Fatalf("loadDistinctArtists = %d artists, %v; want the seeded artist", len(artists), err)
	}
}
