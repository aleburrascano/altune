package persistence

import (
	"context"
	"testing"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPlaylistForDB(t *testing.T, userId shared.UserId) *domain.Playlist {
	t.Helper()
	pl, err := domain.NewPlaylist(userId, "Playlist-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("newTestPlaylistForDB: %v", err)
	}
	return pl
}

func cleanupPlaylist(t *testing.T, pool *pgxpool.Pool, id domain.PlaylistId, userId shared.UserId) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM playlist_tracks WHERE playlist_id = $1`, id.UUID())
		_, _ = pool.Exec(ctx, `DELETE FROM playlists WHERE id = $1 AND user_id = $2`, id.UUID(), userId.UUID())
	})
}

func TestPgxPlaylistRepo_CreateAndGetByID(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)

	// Act: Create
	if err := repo.Create(ctx, pl); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Act: GetByID
	got, _, err := repo.GetByID(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetByID() returned nil, want playlist")
	}

	// Assert
	if got.ID.UUID() != pl.ID.UUID() {
		t.Errorf("ID = %v, want %v", got.ID.UUID(), pl.ID.UUID())
	}
	if got.UserId.UUID() != userId.UUID() {
		t.Errorf("UserId = %v, want %v", got.UserId.UUID(), userId.UUID())
	}
	if got.Name != pl.Name {
		t.Errorf("Name = %q, want %q", got.Name, pl.Name)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}
}

func TestPgxPlaylistRepo_ListForUser(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// Arrange: create 3 playlists with staggered times
	for i := 0; i < 3; i++ {
		pl := newTestPlaylistForDB(t, userId)
		pl.CreatedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		pl.UpdatedAt = pl.CreatedAt
		cleanupPlaylist(t, pool, pl.ID, userId)
		if err := repo.Create(ctx, pl); err != nil {
			t.Fatalf("Create playlist %d: %v", i, err)
		}
	}

	// Act
	got, _, err := repo.ListForUser(ctx, userId)
	if err != nil {
		t.Fatalf("ListForUser() error = %v", err)
	}

	// Assert
	if len(got) != 3 {
		t.Fatalf("len(playlists) = %d, want 3", len(got))
	}

	// Verify descending created_at order
	for i := 1; i < len(got); i++ {
		if got[i-1].CreatedAt.Before(got[i].CreatedAt) {
			t.Errorf("playlists not in descending created_at order at index %d", i)
		}
	}
}

func TestPgxPlaylistRepo_Delete(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)

	if err := repo.Create(ctx, pl); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Act: delete
	deleted, err := repo.Delete(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !deleted {
		t.Error("Delete() deleted = false, want true")
	}

	// Assert: gone
	got, _, err := repo.GetByID(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetByID after delete: %v", err)
	}
	if got != nil {
		t.Errorf("GetByID after delete returned non-nil: %v", got.ID)
	}

	// Assert: deleting again returns false
	deleted2, err := repo.Delete(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if deleted2 {
		t.Error("second Delete() deleted = true, want false (already gone)")
	}
}

func TestPgxPlaylistRepo_AddAndRemoveTrack(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	trackRepo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// Arrange: create a playlist and a track
	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := playlistRepo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := trackRepo.Add(ctx, track); err != nil {
		t.Fatalf("Add track: %v", err)
	}

	// Act: add track to playlist
	if err := playlistRepo.AddTrack(ctx, pl.ID, track.ID, 0); err != nil {
		t.Fatalf("AddTrack() error = %v", err)
	}

	// Assert: verify track appears in GetWithTracks
	gotPl, gotTracks, err := playlistRepo.GetWithTracks(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetWithTracks() error = %v", err)
	}
	if gotPl == nil {
		t.Fatal("GetWithTracks() returned nil playlist")
	}
	if len(gotTracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1", len(gotTracks))
	}
	if gotTracks[0].ID.UUID() != track.ID.UUID() {
		t.Errorf("track ID = %v, want %v", gotTracks[0].ID.UUID(), track.ID.UUID())
	}
	if len(gotPl.Tracks) != 1 {
		t.Fatalf("len(playlist.Tracks) = %d, want 1", len(gotPl.Tracks))
	}
	if gotPl.Tracks[0].Position != 0 {
		t.Errorf("track position = %d, want 0", gotPl.Tracks[0].Position)
	}

	// Act: remove track from playlist
	if err := playlistRepo.RemoveTrack(ctx, pl.ID, track.ID); err != nil {
		t.Fatalf("RemoveTrack() error = %v", err)
	}

	// Assert: no tracks after removal
	gotPl2, gotTracks2, err := playlistRepo.GetWithTracks(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetWithTracks after remove: %v", err)
	}
	if gotPl2 == nil {
		t.Fatal("GetWithTracks after remove returned nil playlist")
	}
	if len(gotTracks2) != 0 {
		t.Errorf("len(tracks) after remove = %d, want 0", len(gotTracks2))
	}
}

func TestPgxPlaylistRepo_ReorderTracks(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	trackRepo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// Arrange: create playlist + 3 tracks
	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := playlistRepo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}

	trackIDs := make([]domain.TrackId, 3)
	for i := 0; i < 3; i++ {
		track := newTestTrackForDB(t, userId)
		cleanupTrack(t, pool, track.ID, userId)
		if _, _, err := trackRepo.Add(ctx, track); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
		if err := playlistRepo.AddTrack(ctx, pl.ID, track.ID, i); err != nil {
			t.Fatalf("AddTrack %d: %v", i, err)
		}
		trackIDs[i] = track.ID
	}

	// Act: reverse the order (2, 1, 0)
	reordered := []domain.PlaylistTrack{
		{TrackId: trackIDs[2], Position: 0},
		{TrackId: trackIDs[1], Position: 1},
		{TrackId: trackIDs[0], Position: 2},
	}
	if err := playlistRepo.ReorderTracks(ctx, pl.ID, reordered); err != nil {
		t.Fatalf("ReorderTracks() error = %v", err)
	}

	// Assert: positions reflect new order
	gotPl, _, err := playlistRepo.GetWithTracks(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetWithTracks after reorder: %v", err)
	}
	if len(gotPl.Tracks) != 3 {
		t.Fatalf("len(tracks) = %d, want 3", len(gotPl.Tracks))
	}

	// GetWithTracks orders by position ASC, so index 0 should be trackIDs[2]
	if gotPl.Tracks[0].TrackId.UUID() != trackIDs[2].UUID() {
		t.Errorf("position 0: track = %v, want %v", gotPl.Tracks[0].TrackId.UUID(), trackIDs[2].UUID())
	}
	if gotPl.Tracks[1].TrackId.UUID() != trackIDs[1].UUID() {
		t.Errorf("position 1: track = %v, want %v", gotPl.Tracks[1].TrackId.UUID(), trackIDs[1].UUID())
	}
	if gotPl.Tracks[2].TrackId.UUID() != trackIDs[0].UUID() {
		t.Errorf("position 2: track = %v, want %v", gotPl.Tracks[2].TrackId.UUID(), trackIDs[0].UUID())
	}
}

func TestPgxPlaylistRepo_GetByID_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	got, _, err := repo.GetByID(ctx, domain.PlaylistIdFromUUID(uuid.New()), userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got != nil {
		t.Errorf("GetByID() for missing playlist returned non-nil: %v", got.ID)
	}
}
