package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
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

	_, created, err := repo.Add(ctx, track)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !created {
		t.Fatal("Add() created = false, want true")
	}

	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetByID() returned nil, want track")
	}

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

	track2, err := domain.NewTrack(userId, track1.Title, track1.Artist, track1.Album)
	if err != nil {
		t.Fatalf("NewTrack for dedup: %v", err)
	}
	cleanupTrack(t, pool, track2.ID, userId)

	_, created2, err := repo.Add(ctx, track2)
	if err != nil {
		t.Fatalf("second Add() error = %v", err)
	}

	if created2 {
		t.Error("second Add() created = true, want false (dedup conflict)")
	}
}

func TestPgxTrackRepo_ListForUser(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	tracks := make([]*domain.Track, 3)
	for i := 0; i < 3; i++ {
		tracks[i] = newTestTrackForDB(t, userId)
		tracks[i].AddedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		cleanupTrack(t, pool, tracks[i].ID, userId)
		if _, _, err := repo.Add(ctx, tracks[i]); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
	}

	got, total, err := repo.ListForUser(ctx, userId, 2, 0)
	if err != nil {
		t.Fatalf("ListForUser() error = %v", err)
	}

	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(got) != 2 {
		t.Errorf("len(tracks) = %d, want 2", len(got))
	}

	if len(got) >= 2 && got[0].AddedAt.Before(got[1].AddedAt) {
		t.Error("tracks not in descending added_at order")
	}

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

	audioRef := "s3://bucket/test-" + uuid.New().String() + ".opus"
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if err := repo.Update(ctx, track); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

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

	deleted, _, err := repo.Delete(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !deleted {
		t.Error("Delete() deleted = false, want true")
	}

	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByID after delete: %v", err)
	}
	if got != nil {
		t.Errorf("GetByID after delete returned non-nil track: %v", got.ID)
	}

	deleted2, _, err := repo.Delete(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if deleted2 {
		t.Error("second Delete() deleted = true, want false (already gone)")
	}
}

func TestPgxTrackRepo_Delete_CrossTenantIDOR(t *testing.T) {
	pool := testPool(t)
	trackRepo := NewPgxTrackRepository(pool)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()

	userB := shared.NewUserId(uuid.New())
	userA := shared.NewUserId(uuid.New())

	trackB := newTestTrackForDB(t, userB)
	cleanupTrack(t, pool, trackB.ID, userB)
	if _, _, err := trackRepo.Add(ctx, trackB); err != nil {
		t.Fatalf("Add track B: %v", err)
	}

	plB := newTestPlaylistForDB(t, userB)
	cleanupPlaylist(t, pool, plB.ID, userB)
	if err := playlistRepo.Create(ctx, plB); err != nil {
		t.Fatalf("Create playlist B: %v", err)
	}
	if err := playlistRepo.AddTrack(ctx, plB.ID, trackB.ID, 0); err != nil {
		t.Fatalf("AddTrack B: %v", err)
	}

	deleted, _, err := trackRepo.Delete(ctx, trackB.ID, userA)
	if err != nil {
		t.Fatalf("Delete(trackB, userA) error = %v", err)
	}
	if deleted {
		t.Error("Delete(trackB, userA) deleted = true, want false (not owner)")
	}

	got, err := trackRepo.GetByID(ctx, trackB.ID, userB)
	if err != nil {
		t.Fatalf("GetByID after cross-tenant delete: %v", err)
	}
	if got == nil {
		t.Fatal("B's track vanished after A's delete attempt")
	}

	_, gotTracks, err := playlistRepo.GetWithTracks(ctx, plB.ID, userB)
	if err != nil {
		t.Fatalf("GetWithTracks after cross-tenant delete: %v", err)
	}
	if len(gotTracks) != 1 {
		t.Fatalf("B's playlist has %d tracks after A's delete, want 1", len(gotTracks))
	}
	if gotTracks[0].ID.UUID() != trackB.ID.UUID() {
		t.Errorf("B's playlist track = %v, want %v", gotTracks[0].ID.UUID(), trackB.ID.UUID())
	}
}

// rawPlaylistPositions reads the stored playlist_tracks.position values for a
// playlist ordered by track_id, bypassing GetWithTracks (which reassigns
// contiguous indices and would therefore mask a renumbering bug).
func rawPlaylistPositions(t *testing.T, pool *pgxpool.Pool, playlistId domain.PlaylistId) map[uuid.UUID]int {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT track_id, position FROM playlist_tracks WHERE playlist_id = $1`,
		playlistId.UUID())
	if err != nil {
		t.Fatalf("rawPlaylistPositions query: %v", err)
	}
	defer rows.Close()

	positions := map[uuid.UUID]int{}
	for rows.Next() {
		var (
			trackId uuid.UUID
			pos     int
		)
		if err := rows.Scan(&trackId, &pos); err != nil {
			t.Fatalf("rawPlaylistPositions scan: %v", err)
		}
		positions[trackId] = pos
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rawPlaylistPositions rows: %v", err)
	}
	return positions
}

// TestPgxTrackRepo_Delete_EvictsFromAllPlaylists pins the cross-aggregate side
// effect of a track delete: the track is removed from every playlist that
// references it, while every other membership row is left untouched. Eviction
// is owned by the playlist_tracks -> tracks ON DELETE CASCADE (migration 001),
// so the track repository does not renumber surviving positions; this test
// deliberately asserts the surviving rows keep their original positions to lock
// the behavior the refactor preserves.
func TestPgxTrackRepo_Delete_EvictsFromAllPlaylists(t *testing.T) {
	pool := testPool(t)
	trackRepo := NewPgxTrackRepository(pool)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	trackA := newTestTrackForDB(t, userId)
	trackB := newTestTrackForDB(t, userId) // the track to delete
	trackC := newTestTrackForDB(t, userId)
	for _, tr := range []*domain.Track{trackA, trackB, trackC} {
		cleanupTrack(t, pool, tr.ID, userId)
		if _, _, err := trackRepo.Add(ctx, tr); err != nil {
			t.Fatalf("Add track: %v", err)
		}
	}

	// P1: A(0), B(1), C(2) — deleting B cascades B out, leaving A(0), C(2).
	p1 := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, p1.ID, userId)
	if err := playlistRepo.Create(ctx, p1); err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	for _, tr := range []*domain.Track{trackA, trackB, trackC} {
		if err := playlistRepo.AddTrack(ctx, p1.ID, tr.ID, 0); err != nil {
			t.Fatalf("AddTrack p1: %v", err)
		}
	}

	// P2: B(0), C(1) — deleting B cascades B out, leaving C(1).
	p2 := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, p2.ID, userId)
	if err := playlistRepo.Create(ctx, p2); err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	for _, tr := range []*domain.Track{trackB, trackC} {
		if err := playlistRepo.AddTrack(ctx, p2.ID, tr.ID, 0); err != nil {
			t.Fatalf("AddTrack p2: %v", err)
		}
	}

	deleted, _, err := trackRepo.Delete(ctx, trackB.ID, userId)
	if err != nil {
		t.Fatalf("Delete(trackB) error = %v", err)
	}
	if !deleted {
		t.Fatal("Delete(trackB) deleted = false, want true")
	}

	p1Pos := rawPlaylistPositions(t, pool, p1.ID)
	// trackA and trackC keep their original positions (0 and 2): the cascade
	// evicts trackB but does not renumber the survivors.
	wantP1 := map[uuid.UUID]int{trackA.ID.UUID(): 0, trackC.ID.UUID(): 2}
	if _, present := p1Pos[trackB.ID.UUID()]; present {
		t.Error("trackB still present in p1 after delete")
	}
	for id, want := range wantP1 {
		if got, ok := p1Pos[id]; !ok || got != want {
			t.Errorf("p1 position for %v = %d (present=%v), want %d", id, got, ok, want)
		}
	}
	if len(p1Pos) != 2 {
		t.Errorf("p1 membership count = %d, want 2", len(p1Pos))
	}

	p2Pos := rawPlaylistPositions(t, pool, p2.ID)
	if _, present := p2Pos[trackB.ID.UUID()]; present {
		t.Error("trackB still present in p2 after delete")
	}
	// trackC keeps its original position (1): the cascade leaves a gap at 0.
	if got, ok := p2Pos[trackC.ID.UUID()]; !ok || got != 1 {
		t.Errorf("p2 position for trackC = %d (present=%v), want 1", got, ok)
	}
	if len(p2Pos) != 1 {
		t.Errorf("p2 membership count = %d, want 1", len(p2Pos))
	}
}

func TestPgxTrackRepo_GetByID_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	got, err := repo.GetByID(ctx, domain.TrackIdFromUUID(uuid.New()), userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got != nil {
		t.Errorf("GetByID() for missing track returned non-nil: %v", got.ID)
	}
}

func TestPgxTrackRepo_ListOwnedTrackRefs_BoundedByLimit(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	prev := maxOwnedTrackRefs
	maxOwnedTrackRefs = 3
	t.Cleanup(func() { maxOwnedTrackRefs = prev })

	const inserted = 5
	for i := 0; i < inserted; i++ {
		track := newTestTrackForDB(t, userId)
		cleanupTrack(t, pool, track.ID, userId)
		if _, _, err := repo.Add(ctx, track); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
	}

	refs, err := repo.ListOwnedTrackRefs(ctx, userId)
	if err != nil {
		t.Fatalf("ListOwnedTrackRefs() error = %v", err)
	}
	if len(refs) != maxOwnedTrackRefs {
		t.Fatalf("len(refs) = %d, want %d (bounded by limit, %d inserted)",
			len(refs), maxOwnedTrackRefs, inserted)
	}
}
