package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

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
				if !strings.Contains(err.Error(), tt.wantErr) {
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

// withPlaylistCap lowers the per-user playlist cap for one test, so crossing
// it costs a handful of rows rather than a thousand.
func withPlaylistCap(t *testing.T, limit int) {
	t.Helper()
	prev := maxPlaylistsPerUser
	maxPlaylistsPerUser = limit
	t.Cleanup(func() { maxPlaylistsPerUser = prev })
}

// Playlist names need not be distinct and the list is paged, so before #2200
// one account could create playlists without limit. The create that would
// cross the cap is refused, and stores nothing.
func TestPlaylistLifecycleService_Create_RejectsPastUserCap(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	plRepo := catalogtest.NewPlaylistRepo()
	withPlaylistCap(t, 2)
	for i := range maxPlaylistsPerUser {
		seedPlaylist(t, plRepo, userId, fmt.Sprintf("Held %d", i))
	}
	svc := NewPlaylistLifecycleService(plRepo)

	playlist, err := svc.Create(ctx, userId, "One Too Many")

	if !errors.Is(err, ErrTooManyPlaylists) {
		t.Fatalf("error = %v, want ErrTooManyPlaylists", err)
	}
	if playlist != nil {
		t.Fatalf("playlist = %+v, want nil", playlist)
	}
	if len(plRepo.Playlists) != maxPlaylistsPerUser {
		t.Fatalf("stored playlists = %d, want %d: the refused create must not insert",
			len(plRepo.Playlists), maxPlaylistsPerUser)
	}
}

// The cap counts the caller's own rows: another owner at the cap may not
// refuse this account's create.
func TestPlaylistLifecycleService_Create_CapCountsOnlyTheCallersPlaylists(t *testing.T) {
	ctx := context.Background()
	plRepo := catalogtest.NewPlaylistRepo()
	withPlaylistCap(t, 2)
	for i := range maxPlaylistsPerUser {
		seedPlaylist(t, plRepo, testOtherUserId(), fmt.Sprintf("Theirs %d", i))
	}
	svc := NewPlaylistLifecycleService(plRepo)

	playlist, err := svc.Create(ctx, testUserId(), "Mine")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if playlist == nil {
		t.Fatal("playlist = nil, want created: another owner's rows are not this caller's cap")
	}
}

func TestPlaylistLifecycleService_Create_StampsWallClockInUTC(t *testing.T) {
	svc := NewPlaylistLifecycleService(catalogtest.NewPlaylistRepo())

	before := time.Now()
	pl, err := svc.Create(context.Background(), testUserId(), "Clocked")
	after := time.Now()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if pl.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", pl.CreatedAt.Location())
	}
	if pl.CreatedAt.Before(before) || pl.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want within [%v, %v]", pl.CreatedAt, before, after)
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
				track, _ := domain.NewTrack(userId, "Track", "Artist", "Album")
				repo.SeedWithTracks(pl, []*domain.Track{track})
				return pl.ID
			},
			wantTracks: 1,
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
				if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
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
				if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
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

			_, _, err := svc.Rename(ctx, userId, playlistId, tt.newName)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			renamed, _, _ := plRepo.GetByID(ctx, playlistId, userId)
			if renamed == nil {
				t.Fatal("expected playlist to still exist after rename")
			}
			if renamed.Name != tt.newName {
				t.Errorf("Name after rename = %q, want %q", renamed.Name, tt.newName)
			}
		})
	}
}

// playlistDeletedAfterRead answers GetByID with the playlist and then deletes
// it, so the rename's write arrives after the row is gone.
type playlistDeletedAfterRead struct {
	*catalogtest.PlaylistRepo
}

func (r *playlistDeletedAfterRead) GetByID(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, domain.PlaylistSummary, error) {
	playlist, summary, err := r.PlaylistRepo.GetByID(ctx, id, userId)
	if playlist == nil || err != nil {
		return playlist, summary, err
	}
	if _, err := r.Delete(ctx, id, userId); err != nil {
		return nil, domain.PlaylistSummary{}, err
	}
	return playlist, summary, nil
}

// Rename reads the playlist, then writes it, and a delete can commit in
// between (issue #2197). The write then matches no row, so the rename must
// answer not-found rather than report success and announce a rename of a
// playlist nobody can read.
func TestPlaylistLifecycleService_Rename_RefusesAPlaylistDeletedAfterTheRead(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	plRepo := catalogtest.NewPlaylistRepo()
	pl := seedPlaylist(t, plRepo, userId, "Old Name")
	pub := &recordingPlaylistPublisher{}
	svc := NewPlaylistLifecycleService(&playlistDeletedAfterRead{plRepo}, WithPlaylistLifecycleEvents(pub))

	_, _, err := svc.Rename(ctx, userId, pl.ID, "New Name")

	if !errors.Is(err, ErrPlaylistNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrPlaylistNotFound)
	}
	if payload := pub.last(events.TypePlaylistRenamed); payload != nil {
		t.Fatalf("published a rename of a deleted playlist: %v", payload)
	}
}
