package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

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
				track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				return pl.ID, track.ID
			},
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				return domain.NewPlaylistId(), track.ID
			},
			wantErr: ErrPlaylistNotFound,
		},
		{
			name: "track not found returns ErrTrackNotFound",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				return pl.ID, domain.NewTrackId()
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "track already in playlist returns ErrTrackAlreadyInPlaylist",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
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

func TestPlaylistMembershipService_AddTracks(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	t.Run("adds every owned track at contiguous positions", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		first := seedTrack(t, trRepo, userId, "First", "Artist", "Album")
		second := seedTrack(t, trRepo, userId, "Second", "Artist", "Album")
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{first.ID, second.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 2 {
			t.Fatalf("added = %d, want 2", added)
		}
		want := []domain.PlaylistTrack{
			{TrackId: first.ID, Position: 0},
			{TrackId: second.ID, Position: 1},
		}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("appends after the tracks already in the playlist", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		existing := seedTrack(t, trRepo, userId, "Existing", "Artist", "Album")
		fresh := seedTrack(t, trRepo, userId, "Fresh", "Artist", "Album")
		if err := pl.AddTrack(existing.ID); err != nil {
			t.Fatalf("seed AddTrack: %v", err)
		}
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{fresh.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		want := []domain.PlaylistTrack{{TrackId: fresh.ID, Position: 1}}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("skips tracks already in the playlist instead of failing the batch", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		existing := seedTrack(t, trRepo, userId, "Existing", "Artist", "Album")
		fresh := seedTrack(t, trRepo, userId, "Fresh", "Artist", "Album")
		if err := pl.AddTrack(existing.ID); err != nil {
			t.Fatalf("seed AddTrack: %v", err)
		}
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{existing.ID, fresh.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		want := []domain.PlaylistTrack{{TrackId: fresh.ID, Position: 1}}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("skips a duplicate inside one request", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		track := seedTrack(t, trRepo, userId, "Repeated", "Artist", "Album")
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{track.ID, track.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
	})

	t.Run("skips a track the user does not own", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		owned := seedTrack(t, trRepo, userId, "Owned", "Artist", "Album")
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{owned.ID, domain.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		want := []domain.PlaylistTrack{{TrackId: owned.ID, Position: 0}}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("does not touch the repository when nothing is addable", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		plRepo.ErrOnAddTracks = errRepo
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{domain.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 0 {
			t.Fatalf("added = %d, want 0", added)
		}
	})

	t.Run("rejects a batch over the cap", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		ids := make([]domain.TrackId, MaxPlaylistBatchSize+1)
		for i := range ids {
			ids[i] = domain.NewTrackId()
		}
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		if _, err := svc.AddTracks(ctx, userId, pl.ID, ids); err == nil {
			t.Fatal("expected a validation error, got nil")
		}
	})

	t.Run("playlist not found returns ErrPlaylistNotFound", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.AddTracks(ctx, userId, domain.NewPlaylistId(), []domain.TrackId{domain.NewTrackId()})

		if !errors.Is(err, ErrPlaylistNotFound) {
			t.Fatalf("error = %v, want %v", err, ErrPlaylistNotFound)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTracks = errRepo
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{track.ID})

		if !errors.Is(err, errRepo) {
			t.Fatalf("error = %v, want %v", err, errRepo)
		}
	})
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

func TestPlaylistMembershipService_RemoveTracks(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	seedPlaylistWithTracks := func(t *testing.T, plRepo *catalogtest.PlaylistRepo, trackIds ...domain.TrackId) *domain.Playlist {
		t.Helper()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		for _, id := range trackIds {
			if err := pl.AddTrack(id); err != nil {
				t.Fatalf("seed AddTrack: %v", err)
			}
		}
		return pl
	}

	t.Run("removes every listed track in one repository call", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		first, second, third := domain.NewTrackId(), domain.NewTrackId(), domain.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, first, second, third)
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{first, third})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 2 {
			t.Fatalf("removed = %d, want 2", removed)
		}
		if !reflect.DeepEqual(plRepo.Removed, []domain.TrackId{first, third}) {
			t.Fatalf("persisted = %v, want %v", plRepo.Removed, []domain.TrackId{first, third})
		}
	})

	t.Run("leaves the surviving tracks at contiguous positions", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		first, second, third := domain.NewTrackId(), domain.NewTrackId(), domain.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, first, second, third)
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{first}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []domain.PlaylistTrack{
			{TrackId: second, Position: 0},
			{TrackId: third, Position: 1},
		}
		if !reflect.DeepEqual(pl.Tracks, want) {
			t.Fatalf("tracks = %v, want %v", pl.Tracks, want)
		}
	})

	t.Run("skips a track that is not in the playlist rather than failing the batch", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		member := domain.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, member)
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{member, domain.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 1 {
			t.Fatalf("removed = %d, want 1", removed)
		}
	})

	t.Run("does not touch the repository when nothing was a member", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylistWithTracks(t, plRepo)
		plRepo.ErrOnRemoveTracks = errRepo
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{domain.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 0 {
			t.Fatalf("removed = %d, want 0", removed)
		}
	})

	t.Run("rejects a batch over the cap", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		ids := make([]domain.TrackId, MaxPlaylistBatchSize+1)
		for i := range ids {
			ids[i] = domain.NewTrackId()
		}
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, ids); err == nil {
			t.Fatal("expected a validation error, got nil")
		}
	})

	t.Run("playlist not found returns ErrPlaylistNotFound", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.RemoveTracks(ctx, userId, domain.NewPlaylistId(), []domain.TrackId{domain.NewTrackId()})

		if !errors.Is(err, ErrPlaylistNotFound) {
			t.Fatalf("error = %v, want %v", err, ErrPlaylistNotFound)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		member := domain.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, member)
		plRepo.ErrOnRemoveTracks = errRepo
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{member})

		if !errors.Is(err, errRepo) {
			t.Fatalf("error = %v, want %v", err, errRepo)
		}
	})
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
