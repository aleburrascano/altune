package main

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedIdentity(t *testing.T, pool *pgxpool.Pool, userID, status, acquisition string) string {
	t.Helper()
	trackID := uuid.NewString()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, isrc, year, duration_seconds, catalog_duration_seconds, acquisition_status, version)
		 VALUES ($1::uuid, $2, 'Celebration', 'Kool & The Gang', 'Celebrate!', $1::text, 'USPR38007190', 1980, 298, 221, $3, 7)`,
		trackID, userID, acquisition)
	if err != nil {
		t.Fatalf("seed track: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO track_identity (track_id, user_id, isrc, mb_recording_id, status, reason, evidence, run_id, track_version_at_bind)
		 VALUES ($1, $2, 'USPR38007190', 'rec-1', $3, 'test', '{}', $4, 7)`,
		trackID, userID, status, uuid.New())
	if err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM tracks WHERE id = $1", trackID) })
	return trackID
}

func TestLoadBoundTracks_ReadsOnlyBoundReadyTracksWithTheirIdentity(t *testing.T) {
	pool := testPool(t)
	user := uuid.NewString()
	bound := seedIdentity(t, pool, user, "bound", "ready")
	seedIdentity(t, pool, user, "unbound", "ready")
	seedIdentity(t, pool, user, "unofficial", "ready")
	seedIdentity(t, pool, user, "bound", "pending")
	got, err := loadBoundTracks(t.Context(), pool, user, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	track := got[0]
	if track.ID != bound || track.UserID != user || track.Version != 7 || track.FileSeconds != 298 || track.CatalogDurationSeconds != 221 ||
		track.Year != 1980 || track.IdentityISRC != "USPR38007190" || track.MBRecordingID != "rec-1" || track.AlbumArtist != "" {
		t.Fatalf("got %+v", track)
	}
}

func TestLoadBoundTracks_HonorsTheLimit(t *testing.T) {
	pool := testPool(t)
	user := uuid.NewString()
	seedIdentity(t, pool, user, "bound", "ready")
	seedIdentity(t, pool, user, "bound", "ready")
	got, err := loadBoundTracks(t.Context(), pool, user, 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d, %v", len(got), err)
	}
}
