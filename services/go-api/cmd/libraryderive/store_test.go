package main

import (
	"context"
	"os"
	"testing"
	"time"

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

func TestLoadBoundTracksSince_KeepsOnlyTracksAddedOrBoundAfterThatTime(t *testing.T) {
	pool := testPool(t)
	user := uuid.NewString()
	old := seedIdentity(t, pool, user, "bound", "ready")
	newlyAdded := seedIdentity(t, pool, user, "bound", "ready")
	newlyBound := seedIdentity(t, pool, user, "bound", "ready")
	cutoff := time.Now().UTC().Truncate(time.Second)
	before, after := cutoff.Add(-time.Hour), cutoff.Add(time.Hour)
	mustExec(t, pool, "UPDATE tracks SET added_at = $2 WHERE id = ANY($1::uuid[])", []string{old, newlyBound}, before)
	mustExec(t, pool, "UPDATE tracks SET added_at = $2 WHERE id = $1::uuid", newlyAdded, after)
	mustExec(t, pool, "UPDATE track_identity SET bound_at = $2 WHERE track_id = ANY($1::uuid[])", []string{old, newlyAdded}, before)
	mustExec(t, pool, "UPDATE track_identity SET bound_at = $2 WHERE track_id = $1::uuid", newlyBound, after)
	got, err := loadBoundTracksSince(t.Context(), pool, user, 0, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, track := range got {
		ids[track.ID] = true
	}
	if len(ids) != 2 || !ids[newlyAdded] || !ids[newlyBound] || ids[old] {
		t.Fatalf("got %v, want only %s and %s", ids, newlyAdded, newlyBound)
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}
