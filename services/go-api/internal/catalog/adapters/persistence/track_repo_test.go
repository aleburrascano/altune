package persistence

import (
	"context"
	"os"
	"testing"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

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
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func newTestTrackForDB(t *testing.T, userId shared.UserId) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Title-"+uuid.New().String()[:8], "Artist-"+uuid.New().String()[:8], "Album")
	if err != nil {
		t.Fatalf("newTestTrackForDB: %v", err)
	}
	return track
}

func cleanupTrack(t *testing.T, pool *pgxpool.Pool, id domain.TrackId, userId shared.UserId) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM tracks WHERE id = $1 AND user_id = $2`,
			id.UUID(), userId.UUID())
	})
}

func TestPgxTrackRepo_AddAndGetByID(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)

	// Act: Add
	_, created, err := repo.Add(ctx, track)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !created {
		t.Fatal("Add() created = false, want true")
	}

	// Act: GetByID
	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetByID() returned nil, want track")
	}

	// Assert: all persisted fields match
	if got.ID.UUID() != track.ID.UUID() {
		t.Errorf("ID = %v, want %v", got.ID.UUID(), track.ID.UUID())
	}
	if got.UserId.UUID() != userId.UUID() {
		t.Errorf("UserId = %v, want %v", got.UserId.UUID(), userId.UUID())
	}
	if got.Title != track.Title {
		t.Errorf("Title = %q, want %q", got.Title, track.Title)
	}
	if got.Artist != track.Artist {
		t.Errorf("Artist = %q, want %q", got.Artist, track.Artist)
	}
	if got.Album != track.Album {
		t.Errorf("Album = %q, want %q", got.Album, track.Album)
	}
	if got.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("AcquisitionStatus = %v, want pending", got.AcquisitionStatus)
	}
	if got.DedupKey != track.DedupKey {
		t.Errorf("DedupKey = %q, want %q", got.DedupKey, track.DedupKey)
	}
}

func TestPgxTrackRepo_Add_DedupConflict(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track1 := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track1.ID, userId)

	_, created1, err := repo.Add(ctx, track1)
	if err != nil {
		t.Fatalf("first Add() error = %v", err)
	}
	if !created1 {
		t.Fatal("first Add() created = false, want true")
	}

	// Second track with same title/artist/album (same dedup key), different ID
	track2, err := domain.NewTrack(userId, track1.Title, track1.Artist, track1.Album)
	if err != nil {
		t.Fatalf("NewTrack for dedup: %v", err)
	}
	cleanupTrack(t, pool, track2.ID, userId)

	// Act: second Add with same dedup key
	_, created2, err := repo.Add(ctx, track2)
	if err != nil {
		t.Fatalf("second Add() error = %v", err)
	}

	// Assert: dedup conflict returns created=false
	if created2 {
		t.Error("second Add() created = true, want false (dedup conflict)")
	}
}

func TestPgxTrackRepo_ListForUser(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// Arrange: add 3 tracks with staggered times
	tracks := make([]*domain.Track, 3)
	for i := 0; i < 3; i++ {
		tracks[i] = newTestTrackForDB(t, userId)
		tracks[i].AddedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		cleanupTrack(t, pool, tracks[i].ID, userId)
		if _, _, err := repo.Add(ctx, tracks[i]); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
	}

	// Act: list with limit=2, offset=0
	got, total, err := repo.ListForUser(ctx, userId, 2, 0)
	if err != nil {
		t.Fatalf("ListForUser() error = %v", err)
	}

	// Assert
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(got) != 2 {
		t.Errorf("len(tracks) = %d, want 2", len(got))
	}

	// Verify ordering: most recently added first
	if len(got) >= 2 && got[0].AddedAt.Before(got[1].AddedAt) {
		t.Error("tracks not in descending added_at order")
	}

	// Act: list with offset=2 to get the last one
	got2, total2, err := repo.ListForUser(ctx, userId, 10, 2)
	if err != nil {
		t.Fatalf("ListForUser(offset=2) error = %v", err)
	}
	if total2 != 3 {
		t.Errorf("total at offset=2 = %d, want 3", total2)
	}
	if len(got2) != 1 {
		t.Errorf("len(tracks) at offset=2 = %d, want 1", len(got2))
	}
}

func TestPgxTrackRepo_Update(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)

	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Act: mark ready and update
	audioRef := "s3://bucket/test-" + uuid.New().String() + ".opus"
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if err := repo.Update(ctx, track); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Assert: re-read and verify
	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID after update returned nil")
	}
	if got.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("AcquisitionStatus = %v, want ready", got.AcquisitionStatus)
	}
	if got.AudioRef == nil || *got.AudioRef != audioRef {
		t.Errorf("AudioRef = %v, want %q", got.AudioRef, audioRef)
	}
}

func TestPgxTrackRepo_Delete(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)

	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Act: delete
	deleted, _, err := repo.Delete(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !deleted {
		t.Error("Delete() deleted = false, want true")
	}

	// Assert: gone
	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByID after delete: %v", err)
	}
	if got != nil {
		t.Errorf("GetByID after delete returned non-nil track: %v", got.ID)
	}

	// Assert: deleting again returns false
	deleted2, _, err := repo.Delete(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if deleted2 {
		t.Error("second Delete() deleted = true, want false (already gone)")
	}
}

func TestPgxTrackRepo_GetByID_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// Act: get a non-existent track
	got, err := repo.GetByID(ctx, domain.TrackIdFromUUID(uuid.New()), userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got != nil {
		t.Errorf("GetByID() for missing track returned non-nil: %v", got.ID)
	}
}
