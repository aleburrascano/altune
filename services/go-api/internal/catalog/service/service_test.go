package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// --- helpers ---

func testUserId() shared.UserId {
	return shared.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
}

func seedTrack(t *testing.T, repo *mockTrackRepo, userId shared.UserId, title, artist, album string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, title, artist, album)
	if err != nil {
		t.Fatalf("seedTrack: %v", err)
	}
	repo.seed(track)
	return track
}

func seedReadyTrack(t *testing.T, repo *mockTrackRepo, userId shared.UserId, title, artist, album, audioRef string) *domain.Track {
	t.Helper()
	track := seedTrack(t, repo, userId, title, artist, album)
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("seedReadyTrack: %v", err)
	}
	return track
}

func seedPlaylist(t *testing.T, repo *mockPlaylistRepo, userId shared.UserId, name string) *domain.Playlist {
	t.Helper()
	playlist, err := domain.NewPlaylist(userId, name)
	if err != nil {
		t.Fatalf("seedPlaylist: %v", err)
	}
	repo.seed(playlist)
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
		setup       func(*mockTrackRepo)
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
			setup: func(repo *mockTrackRepo) {
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
			wantErr: "track title is required",
		},
		{
			name: "empty artist returns validation error",
			input: AddTrackInput{
				Title:  "Song",
				Artist: "",
				Album:  "Album",
			},
			wantErr: "track artist is required",
		},
		{
			name: "repo error propagates",
			input: AddTrackInput{
				Title:  "Song",
				Artist: "Artist",
				Album:  "Album",
			},
			setup: func(repo *mockTrackRepo) {
				repo.errOnAdd = errRepo
			},
			wantErr: "db connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockTrackRepo()
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
		setup       func(*mockTrackRepo)
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
			setup: func(repo *mockTrackRepo) {
				repo.errOnList = errRepo
			},
			wantErr: "db timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockTrackRepo()
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
		setup   func(*mockTrackRepo) domain.TrackId
		wantErr error
	}{
		{
			name: "existing track is deleted",
			setup: func(repo *mockTrackRepo) domain.TrackId {
				track := seedTrack(t, repo, userId, "Song", "Artist", "Album")
				return track.ID
			},
			wantErr: nil,
		},
		{
			name: "non-existent track returns ErrTrackNotFound",
			setup: func(repo *mockTrackRepo) domain.TrackId {
				return domain.NewTrackId() // not in repo
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *mockTrackRepo) domain.TrackId {
				repo.errOnDelete = errRepo
				return domain.NewTrackId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockTrackRepo()
			trackId := tt.setup(repo)
			svc := NewDeleteTrackService(repo)

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

func TestPlaylistService_Create(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		plName  string
		setup   func(*mockPlaylistRepo)
		wantErr string
	}{
		{
			name:   "valid name creates playlist",
			plName: "My Favorites",
		},
		{
			name:    "empty name returns validation error",
			plName:  "",
			wantErr: "playlist name cannot be empty",
		},
		{
			name:   "repo error propagates",
			plName: "Good Name",
			setup: func(repo *mockPlaylistRepo) {
				repo.errOnCreate = errRepo
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := newMockPlaylistRepo()
			trRepo := newMockTrackRepo()
			if tt.setup != nil {
				tt.setup(plRepo)
			}
			svc := NewPlaylistService(plRepo, trRepo)

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

func TestPlaylistService_Get(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name       string
		setup      func(*mockPlaylistRepo) domain.PlaylistId
		wantTracks int
		wantErr    error
	}{
		{
			name: "found playlist with tracks",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				pl := seedPlaylist(t, repo, userId, "Rock")
				track, _ := domain.NewTrack(userId, "Song", "Artist", "Album")
				repo.seedWithTracks(pl, []*domain.Track{track})
				return pl.ID
			},
			wantTracks: 1,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				return domain.NewPlaylistId() // not seeded
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				repo.errOnGetWithTracks = errRepo
				return domain.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := newMockPlaylistRepo()
			trRepo := newMockTrackRepo()
			playlistId := tt.setup(plRepo)
			svc := NewPlaylistService(plRepo, trRepo)

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

func TestPlaylistService_Delete(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*mockPlaylistRepo) domain.PlaylistId
		wantErr error
	}{
		{
			name: "existing playlist is deleted",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				pl := seedPlaylist(t, repo, userId, "To Delete")
				return pl.ID
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				return domain.NewPlaylistId()
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				repo.errOnDelete = errRepo
				return domain.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := newMockPlaylistRepo()
			trRepo := newMockTrackRepo()
			playlistId := tt.setup(plRepo)
			svc := NewPlaylistService(plRepo, trRepo)

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

func TestPlaylistService_Rename(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		newName string
		setup   func(*mockPlaylistRepo) domain.PlaylistId
		wantErr string
	}{
		{
			name:    "valid rename succeeds",
			newName: "New Name",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				pl := seedPlaylist(t, repo, userId, "Old Name")
				return pl.ID
			},
		},
		{
			name:    "not found returns ErrPlaylistNotFound",
			newName: "New Name",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				return domain.NewPlaylistId()
			},
			wantErr: ErrPlaylistNotFound.Error(),
		},
		{
			name:    "empty name returns validation error",
			newName: "",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				pl := seedPlaylist(t, repo, userId, "Has Name")
				return pl.ID
			},
			wantErr: "playlist name cannot be empty",
		},
		{
			name:    "repo error on GetByID propagates",
			newName: "New Name",
			setup: func(repo *mockPlaylistRepo) domain.PlaylistId {
				repo.errOnGetByID = errRepo
				return domain.NewPlaylistId()
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := newMockPlaylistRepo()
			trRepo := newMockTrackRepo()
			playlistId := tt.setup(plRepo)
			svc := NewPlaylistService(plRepo, trRepo)

			err := svc.Rename(ctx, userId, playlistId, tt.newName)

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

func TestPlaylistService_AddTrack(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name      string
		setup     func(*mockPlaylistRepo, *mockTrackRepo) (domain.PlaylistId, domain.TrackId)
		wantAdded bool
		wantErr   error
	}{
		{
			name: "track added to playlist",
			setup: func(plRepo *mockPlaylistRepo, trRepo *mockTrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				track := seedTrack(t, trRepo, userId, "Song", "Artist", "Album")
				return pl.ID, track.ID
			},
			wantAdded: true,
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *mockPlaylistRepo, trRepo *mockTrackRepo) (domain.PlaylistId, domain.TrackId) {
				track := seedTrack(t, trRepo, userId, "Song", "Artist", "Album")
				return domain.NewPlaylistId(), track.ID
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "track not found returns ErrTrackNotFound",
			setup: func(plRepo *mockPlaylistRepo, trRepo *mockTrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				return pl.ID, domain.NewTrackId() // not in repo
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "track already in playlist returns added=false",
			setup: func(plRepo *mockPlaylistRepo, trRepo *mockTrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				track := seedTrack(t, trRepo, userId, "Song", "Artist", "Album")
				// Add the track to the playlist domain object so AddTrack detects duplicate
				_ = pl.AddTrack(track.ID)
				return pl.ID, track.ID
			},
			wantAdded: false,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *mockPlaylistRepo, trRepo *mockTrackRepo) (domain.PlaylistId, domain.TrackId) {
				plRepo.errOnGetByID = errRepo
				return domain.NewPlaylistId(), domain.NewTrackId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := newMockPlaylistRepo()
			trRepo := newMockTrackRepo()
			playlistId, trackId := tt.setup(plRepo, trRepo)
			svc := NewPlaylistService(plRepo, trRepo)

			added, err := svc.AddTrack(ctx, userId, playlistId, trackId)

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
			if added != tt.wantAdded {
				t.Errorf("added = %v, want %v", added, tt.wantAdded)
			}
		})
	}
}

func TestPlaylistService_RemoveTrack(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*mockPlaylistRepo) (domain.PlaylistId, domain.TrackId)
		wantErr error
	}{
		{
			name: "track removed from playlist",
			setup: func(plRepo *mockPlaylistRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				trackId := domain.NewTrackId()
				return pl.ID, trackId
			},
			wantErr: nil,
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *mockPlaylistRepo) (domain.PlaylistId, domain.TrackId) {
				return domain.NewPlaylistId(), domain.NewTrackId()
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *mockPlaylistRepo) (domain.PlaylistId, domain.TrackId) {
				plRepo.errOnGetByID = errRepo
				return domain.NewPlaylistId(), domain.NewTrackId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := newMockPlaylistRepo()
			trRepo := newMockTrackRepo()
			playlistId, trackId := tt.setup(plRepo)
			svc := NewPlaylistService(plRepo, trRepo)

			_, err := svc.RemoveTrack(ctx, userId, playlistId, trackId)

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

func TestPlaylistService_Reorder(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*mockPlaylistRepo) (domain.PlaylistId, []domain.TrackId)
		wantErr error
	}{
		{
			name: "valid reorder succeeds",
			setup: func(plRepo *mockPlaylistRepo) (domain.PlaylistId, []domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				t1 := domain.NewTrackId()
				t2 := domain.NewTrackId()
				return pl.ID, []domain.TrackId{t2, t1}
			},
			wantErr: nil,
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *mockPlaylistRepo) (domain.PlaylistId, []domain.TrackId) {
				return domain.NewPlaylistId(), []domain.TrackId{}
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *mockPlaylistRepo) (domain.PlaylistId, []domain.TrackId) {
				plRepo.errOnGetByID = errRepo
				return domain.NewPlaylistId(), []domain.TrackId{}
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := newMockPlaylistRepo()
			trRepo := newMockTrackRepo()
			playlistId, trackIds := tt.setup(plRepo)
			svc := NewPlaylistService(plRepo, trRepo)

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

// ==================== ReconcileTrackStatusService ====================

func TestReconcileTrackStatusService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")
	errStore := errors.New("s3 unreachable")

	tests := []struct {
		name          string
		setup         func(*mockTrackRepo, *mockAudioStore) domain.TrackId
		wantErr       error
		wantStatus    *domain.AcquisitionStatus // nil = don't check
		wantAudioRef  *string                   // nil = don't check; empty ptr = expect nil
	}{
		{
			name: "ready track with existing audio is no-op",
			setup: func(trRepo *mockTrackRepo, store *mockAudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Song", "Artist", "Album", "audio/123.opus")
				store.seed("audio/123.opus")
				return track.ID
			},
			wantStatus: ptrStatus(domain.AcquisitionReady),
		},
		{
			name: "ready track with missing audio is marked failed",
			setup: func(trRepo *mockTrackRepo, store *mockAudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Song", "Artist", "Album", "audio/gone.opus")
				// Deliberately not seeding the audio store
				return track.ID
			},
			wantStatus: ptrStatus(domain.AcquisitionFailed),
		},
		{
			name: "pending track is no-op",
			setup: func(trRepo *mockTrackRepo, store *mockAudioStore) domain.TrackId {
				track := seedTrack(t, trRepo, userId, "Song", "Artist", "Album")
				return track.ID
			},
			wantStatus: ptrStatus(domain.AcquisitionPending),
		},
		{
			name: "track not found returns ErrTrackNotFound",
			setup: func(trRepo *mockTrackRepo, store *mockAudioStore) domain.TrackId {
				return domain.NewTrackId()
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "audio store error logs and returns nil",
			setup: func(trRepo *mockTrackRepo, store *mockAudioStore) domain.TrackId {
				track := seedReadyTrack(t, trRepo, userId, "Song", "Artist", "Album", "audio/err.opus")
				store.errOnExists = errStore
				return track.ID
			},
			// Should NOT return error; should silently continue
			wantStatus: ptrStatus(domain.AcquisitionReady),
		},
		{
			name: "repo GetByID error propagates",
			setup: func(trRepo *mockTrackRepo, store *mockAudioStore) domain.TrackId {
				trRepo.errOnGetBy = errRepo
				return domain.NewTrackId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trRepo := newMockTrackRepo()
			store := newMockAudioStore()
			trackId := tt.setup(trRepo, store)
			svc := NewReconcileTrackStatusService(trRepo, store)

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

			// Verify final track status if requested
			if tt.wantStatus != nil {
				track, _ := trRepo.GetByID(ctx, trackId, userId)
				if track == nil {
					t.Fatal("expected track to still exist in repo")
				}
				if track.AcquisitionStatus != *tt.wantStatus {
					t.Errorf("AcquisitionStatus = %v, want %v", track.AcquisitionStatus, *tt.wantStatus)
				}
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
