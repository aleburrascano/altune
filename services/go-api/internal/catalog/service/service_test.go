package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// --- helpers ---

func testUserId() shared.UserId {
	return shared.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
}

func seedTrack(t *testing.T, repo *catalogtest.TrackRepo, userId shared.UserId, title, artist, album string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, title, artist, album)
	if err != nil {
		t.Fatalf("seedTrack: %v", err)
	}
	repo.Seed(track)
	return track
}

func seedReadyTrack(t *testing.T, repo *catalogtest.TrackRepo, userId shared.UserId, title, artist, album, audioRef string) *domain.Track {
	t.Helper()
	track := seedTrack(t, repo, userId, title, artist, album)
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("seedReadyTrack: %v", err)
	}
	return track
}

func seedPlaylist(t *testing.T, repo *catalogtest.PlaylistRepo, userId shared.UserId, name string) *domain.Playlist {
	t.Helper()
	playlist, err := domain.NewPlaylist(userId, name)
	if err != nil {
		t.Fatalf("seedPlaylist: %v", err)
	}
	repo.Seed(playlist)
	return playlist
}

// ==================== AddTrackService ====================

func TestAddTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db connection lost")

	tests := []struct {
		name        string
		input       AddTrackInput
		setup       func(*catalogtest.TrackRepo)
		wantCreated bool
		wantTitle   string
		wantErr     string
	}{
		{
			name: "new track is created",
			input: AddTrackInput{
				Title:  "Song",
				Artist: "Artist",
				Album:  "Album",
			},
			wantCreated: true,
			wantTitle:   "Song",
		},
		{
			name: "duplicate returns existing track not created",
			input: AddTrackInput{
				Title:  "Existing",
				Artist: "Artist",
				Album:  "Album",
			},
			setup: func(repo *catalogtest.TrackRepo) {
				seedTrack(t, repo, userId, "Existing", "Artist", "Album")
			},
			wantCreated: false,
			wantTitle:   "Existing",
		},
		{
			name: "empty title returns validation error",
			input: AddTrackInput{
				Title:  "",
				Artist: "Artist",
				Album:  "Album",
			},
			wantErr: "track title required",
		},
		{
			name: "empty artist returns validation error",
			input: AddTrackInput{
				Title:  "Song",
				Artist: "",
				Album:  "Album",
			},
			wantErr: "track artist required",
		},
		{
			name: "repo error propagates",
			input: AddTrackInput{
				Title:  "Song",
				Artist: "Artist",
				Album:  "Album",
			},
			setup: func(repo *catalogtest.TrackRepo) {
				repo.ErrOnAdd = errRepo
			},
			wantErr: "db connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			if tt.setup != nil {
				tt.setup(repo)
			}
			svc := NewAddTrackService(repo)

			out, err := svc.Execute(ctx, userId, tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Created != tt.wantCreated {
				t.Errorf("Created = %v, want %v", out.Created, tt.wantCreated)
			}
			if out.Track == nil {
				t.Fatal("expected non-nil Track in output")
			}
			if out.Track.Title != tt.wantTitle {
				t.Errorf("Track.Title = %q, want %q", out.Track.Title, tt.wantTitle)
			}
		})
	}
}

// ==================== ListTracksService ====================

func TestListTracksService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db timeout")

	tests := []struct {
		name        string
		limit       int
		offset      int
		seedCount   int
		setup       func(*catalogtest.TrackRepo)
		wantLen     int
		wantTotal   int
		wantHasMore bool
		wantErr     string
	}{
		{
			name:        "returns tracks with HasMore=true when more exist",
			limit:       2,
			offset:      0,
			seedCount:   3,
			wantLen:     2,
			wantTotal:   3,
			wantHasMore: true,
		},
		{
			name:        "returns tracks with HasMore=false when at end",
			limit:       10,
			offset:      0,
			seedCount:   3,
			wantLen:     3,
			wantTotal:   3,
			wantHasMore: false,
		},
		{
			name:        "default limit applied when zero",
			limit:       0,
			offset:      0,
			seedCount:   2,
			wantLen:     2,
			wantTotal:   2,
			wantHasMore: false,
		},
		{
			name:        "negative limit treated as default",
			limit:       -5,
			offset:      0,
			seedCount:   1,
			wantLen:     1,
			wantTotal:   1,
			wantHasMore: false,
		},
		{
			name:        "limit capped at 2000",
			limit:       5000,
			offset:      0,
			seedCount:   1,
			wantLen:     1,
			wantTotal:   1,
			wantHasMore: false,
		},
		{
			name:      "repo error propagates",
			limit:     10,
			offset:    0,
			seedCount: 0,
			setup: func(repo *catalogtest.TrackRepo) {
				repo.ErrOnList = errRepo
			},
			wantErr: "db timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			if tt.setup != nil {
				tt.setup(repo)
			}
			for i := 0; i < tt.seedCount; i++ {
				seedTrack(t, repo, userId, "Track "+string(rune('A'+i)), "Artist", "Album")
			}
			svc := NewListTracksService(repo)

			out, err := svc.Execute(ctx, userId, tt.limit, tt.offset)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(out.Tracks) != tt.wantLen {
				t.Errorf("len(Tracks) = %d, want %d", len(out.Tracks), tt.wantLen)
			}
			if out.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", out.Total, tt.wantTotal)
			}
			if out.HasMore != tt.wantHasMore {
				t.Errorf("HasMore = %v, want %v", out.HasMore, tt.wantHasMore)
			}
		})
	}
}

// ==================== DeleteTrackService ====================

func TestDeleteTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalogtest.TrackRepo) domain.TrackId
		wantErr error
	}{
		{
			name: "existing track is deleted",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				track := seedTrack(t, repo, userId, "Song", "Artist", "Album")
				return track.ID
			},
			wantErr: nil,
		},
		{
			name: "non-existent track returns ErrTrackNotFound",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				return domain.NewTrackId() // not in repo
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				track := seedTrack(t, repo, userId, "Song", "Artist", "Album")
				repo.ErrOnDelete = errRepo
				return track.ID
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			trackId := tt.setup(repo)
			svc := NewDeleteTrackService(repo, catalogtest.NewAudioStore())

			err := svc.Execute(ctx, userId, trackId)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && !contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ==================== PlaylistService ====================

func TestPlaylistLifecycleService_Create(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		plName  string
		setup   func(*catalogtest.PlaylistRepo)
		wantErr string
	}{
		{
			name:   "valid name creates playlist",
			plName: "My Favorites",
		},
		{
			name:    "empty name returns validation error",
			plName:  "",
			wantErr: "playlist name required",
		},
		{
			name:   "repo error propagates",
			plName: "Good Name",
			setup: func(repo *catalogtest.PlaylistRepo) {
				repo.ErrOnCreate = errRepo
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			if tt.setup != nil {
				tt.setup(plRepo)
			}
			svc := NewPlaylistLifecycleService(plRepo)

			playlist, err := svc.Create(ctx, userId, tt.plName)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if playlist == nil {
				t.Fatal("expected non-nil playlist")
			}
			if playlist.Name != tt.plName {
				t.Errorf("Name = %q, want %q", playlist.Name, tt.plName)
			}
			if playlist.ID.IsZero() {
				t.Error("expected non-zero playlist ID")
			}
		})
	}
}

func TestPlaylistLifecycleService_Get(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name       string
		setup      func(*catalogtest.PlaylistRepo) domain.PlaylistId
		wantTracks int
		wantErr    error
	}{
		{
			name: "found playlist with tracks",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				pl := seedPlaylist(t, repo, userId, "Rock")
				track, _ := domain.NewTrack(userId, "Song", "Artist", "Album")
				repo.SeedWithTracks(pl, []*domain.Track{track})
				return pl.ID
			},
			wantTracks: 1,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				return domain.NewPlaylistId() // not seeded
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				repo.ErrOnGetWithTracks = errRepo
				return domain.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := NewPlaylistLifecycleService(plRepo)

			playlist, tracks, err := svc.Get(ctx, userId, playlistId)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && !contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if playlist == nil {
				t.Fatal("expected non-nil playlist")
			}
			if len(tracks) != tt.wantTracks {
				t.Errorf("len(tracks) = %d, want %d", len(tracks), tt.wantTracks)
			}
		})
	}
}

func TestPlaylistLifecycleService_Delete(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalogtest.PlaylistRepo) domain.PlaylistId
		wantErr error
	}{
		{
			name: "existing playlist is deleted",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				pl := seedPlaylist(t, repo, userId, "To Delete")
				return pl.ID
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				return domain.NewPlaylistId()
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				repo.ErrOnDelete = errRepo
				return domain.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := NewPlaylistLifecycleService(plRepo)

			err := svc.Delete(ctx, userId, playlistId)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && !contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPlaylistLifecycleService_Rename(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		newName string
		setup   func(*catalogtest.PlaylistRepo) domain.PlaylistId
		wantErr string
	}{
		{
			name:    "valid rename succeeds",
			newName: "New Name",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				pl := seedPlaylist(t, repo, userId, "Old Name")
				return pl.ID
			},
		},
		{
			name:    "not found returns ErrPlaylistNotFound",
			newName: "New Name",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				return domain.NewPlaylistId()
			},
			wantErr: ErrPlaylistNotFound.Error(),
		},
		{
			name:    "empty name returns validation error",
			newName: "",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				pl := seedPlaylist(t, repo, userId, "Has Name")
				return pl.ID
			},
			wantErr: "playlist name required",
		},
		{
			name:    "repo error on GetByID propagates",
			newName: "New Name",
			setup: func(repo *catalogtest.PlaylistRepo) domain.PlaylistId {
				repo.ErrOnGetByID = errRepo
				return domain.NewPlaylistId()
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := NewPlaylistLifecycleService(plRepo)

			_, err := svc.Rename(ctx, userId, playlistId, tt.newName)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Verify the name was actually updated in the repo
			renamed, _ := plRepo.GetByID(ctx, playlistId, userId)
			if renamed == nil {
				t.Fatal("expected playlist to still exist after rename")
			}
			if renamed.Name != tt.newName {
				t.Errorf("Name after rename = %q, want %q", renamed.Name, tt.newName)
			}
		})
	}
}

func TestPlaylistMembershipService_AddTrack(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalogtest.PlaylistRepo, *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId)
		wantErr error
	}{
		{
			name: "track added to playlist",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				track := seedTrack(t, trRepo, userId, "Song", "Artist", "Album")
				return pl.ID, track.ID
			},
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				track := seedTrack(t, trRepo, userId, "Song", "Artist", "Album")
				return domain.NewPlaylistId(), track.ID
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "track not found returns ErrTrackNotFound",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				return pl.ID, domain.NewTrackId() // not in repo
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "track already in playlist returns ErrTrackAlreadyInPlaylist",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				track := seedTrack(t, trRepo, userId, "Song", "Artist", "Album")
				_ = pl.AddTrack(track.ID)
				return pl.ID, track.ID
			},
			wantErr: domain.ErrTrackAlreadyInPlaylist,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				plRepo.ErrOnGetWithTracks = errRepo
				return domain.NewPlaylistId(), domain.NewTrackId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId, trackId := tt.setup(plRepo, trRepo)
			svc := NewPlaylistMembershipService(plRepo, trRepo)

			err := svc.AddTrack(ctx, userId, playlistId, trackId)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && !contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPlaylistMembershipService_RemoveTrack(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalogtest.PlaylistRepo) (domain.PlaylistId, domain.TrackId)
		wantErr error
	}{
		{
			name: "track removed from playlist",
			setup: func(plRepo *catalogtest.PlaylistRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				trackId := domain.NewTrackId()
				return pl.ID, trackId
			},
			wantErr: nil,
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *catalogtest.PlaylistRepo) (domain.PlaylistId, domain.TrackId) {
				return domain.NewPlaylistId(), domain.NewTrackId()
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *catalogtest.PlaylistRepo) (domain.PlaylistId, domain.TrackId) {
				// RemoveTrack now loads via GetWithTracks (it routes through the
				// aggregate), so the repo error surfaces there.
				plRepo.ErrOnGetWithTracks = errRepo
				return domain.NewPlaylistId(), domain.NewTrackId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId, trackId := tt.setup(plRepo)
			svc := NewPlaylistMembershipService(plRepo, trRepo)

			err := svc.RemoveTrack(ctx, userId, playlistId, trackId)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && !contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPlaylistMembershipService_Reorder(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalogtest.PlaylistRepo) (domain.PlaylistId, []domain.TrackId)
		wantErr error
	}{
		{
			name: "valid reorder succeeds",
			setup: func(plRepo *catalogtest.PlaylistRepo) (domain.PlaylistId, []domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				t1 := domain.NewTrackId()
				t2 := domain.NewTrackId()
				pl.Tracks = []domain.PlaylistTrack{
					{TrackId: t1, Position: 0},
					{TrackId: t2, Position: 1},
				}
				plRepo.Seed(pl)
				return pl.ID, []domain.TrackId{t2, t1}
			},
			wantErr: nil,
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *catalogtest.PlaylistRepo) (domain.PlaylistId, []domain.TrackId) {
				return domain.NewPlaylistId(), []domain.TrackId{}
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *catalogtest.PlaylistRepo) (domain.PlaylistId, []domain.TrackId) {
				plRepo.ErrOnGetWithTracks = errRepo
				return domain.NewPlaylistId(), []domain.TrackId{}
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId, trackIds := tt.setup(plRepo)
			svc := NewPlaylistMembershipService(plRepo, trRepo)

			err := svc.Reorder(ctx, userId, playlistId, trackIds)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && !contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// --- test utilities ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func ptrStatus(s domain.AcquisitionStatus) *domain.AcquisitionStatus {
	return &s
}
