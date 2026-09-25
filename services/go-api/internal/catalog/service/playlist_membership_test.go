package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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
			name: "track deleted before the insert returns ErrTrackNotFound",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				plRepo.ErrOnAddTrack = ports.ErrTrackMissing
				return pl.ID, track.ID
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "track already in playlist returns ErrTrackAlreadyInPlaylist",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				_ = pl.AddTrack(track.ID, time.Now())
				return pl.ID, track.ID
			},
			wantErr: domain.ErrTrackAlreadyInPlaylist,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				plRepo.ErrOnExists = errRepo
				return domain.NewPlaylistId(), domain.NewTrackId()
			},
			wantErr: errRepo,
		},
		{
			name: "never loads the playlist's track list",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (domain.PlaylistId, domain.TrackId) {
				pl := seedPlaylist(t, plRepo, userId, "My Playlist")
				track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				plRepo.ErrOnGetWithTracks = errRepo
				plRepo.ErrOnGetTrackOrder = errRepo
				return pl.ID, track.ID
			},
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
		if err := pl.AddTrack(existing.ID, time.Now()); err != nil {
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
		if err := pl.AddTrack(existing.ID, time.Now()); err != nil {
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

	t.Run("a track deleted before the insert returns ErrTrackNotFound", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTracks = ports.ErrTrackMissing
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{track.ID})

		if !errors.Is(err, ErrTrackNotFound) {
			t.Fatalf("error = %v, want %v", err, ErrTrackNotFound)
		}
	})
}

// assertPlaylistFull checks err is the cap refusal itself: the same error, and
// the same message, so no internal op name has been prefixed onto the detail
// the client renders.
func assertPlaylistFull(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, domain.ErrPlaylistFull) {
		t.Fatalf("error = %v, want domain.ErrPlaylistFull", err)
	}
	if err.Error() != domain.ErrPlaylistFull.Error() {
		t.Fatalf("detail = %q, want %q", err.Error(), domain.ErrPlaylistFull.Error())
	}
}

// TestPlaylistMembershipService_AddPastTheCap_SurfacesTheRefusalWhole covers
// the service half of #2196: only the data layer can tell that an add crosses
// domain.MaxPlaylistTracks, so its refusal is the answer, and the service
// hands it on rather than wrapping it as a fault of its own.
func TestPlaylistMembershipService_AddPastTheCap_SurfacesTheRefusalWhole(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	t.Run("AddTrack", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTrack = domain.ErrPlaylistFull
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		err := svc.AddTrack(ctx, userId, pl.ID, track.ID)

		assertPlaylistFull(t, err)
	})

	t.Run("AddTracks", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := seedPlaylist(t, plRepo, userId, "My Playlist")
		track := seedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTracks = domain.ErrPlaylistFull
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{track.ID})

		assertPlaylistFull(t, err)
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
				plRepo.ErrOnRemoveTrack = errRepo
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
			if err := pl.AddTrack(id, time.Now()); err != nil {
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

	t.Run("removes nothing and reports zero when nothing was a member", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		member := domain.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, member)
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{domain.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 0 || len(plRepo.Removed) != 0 {
			t.Fatalf("removed = %d (persisted %v), want 0", removed, plRepo.Removed)
		}
		if want := []domain.PlaylistTrack{{TrackId: member, Position: 0}}; !reflect.DeepEqual(pl.Tracks, want) {
			t.Fatalf("tracks = %v, want %v", pl.Tracks, want)
		}
	})

	t.Run("never loads the playlist's track list", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		first, second := domain.NewTrackId(), domain.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, first, second)
		plRepo.ErrOnGetWithTracks = errRepo
		plRepo.ErrOnGetTrackOrder = errRepo
		svc := NewPlaylistMembershipService(plRepo, trRepo)

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{first}); err != nil {
			t.Fatalf("RemoveTracks: %v", err)
		}
		if err := svc.RemoveTrack(ctx, userId, pl.ID, second); err != nil {
			t.Fatalf("RemoveTrack: %v", err)
		}
		if len(pl.Tracks) != 0 {
			t.Fatalf("tracks = %v, want none", pl.Tracks)
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
				plRepo.ErrOnGetTrackOrder = errRepo
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

// playlistEditedAfterRead answers GetTrackOrder with the order it read and
// then applies another request's edit — one track removed, one added — so the
// plan the service builds is already stale when it reaches the write.
type playlistEditedAfterRead struct {
	*catalogtest.PlaylistRepo
	added domain.TrackId
}

func (r *playlistEditedAfterRead) GetTrackOrder(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId) ([]domain.TrackId, bool, error) {
	ids, found, err := r.PlaylistRepo.GetTrackOrder(ctx, playlistId, userId)
	if err != nil || !found {
		return ids, found, err
	}
	if _, err := r.RemoveTrack(ctx, userId, playlistId, ids[0]); err != nil {
		return nil, false, err
	}
	if err := r.AddTrack(ctx, userId, playlistId, r.added); err != nil {
		return nil, false, err
	}
	return ids, true, nil
}

// Reorder reads the order without a lock, so a concurrent edit can land before
// the write (issue #2197). The plan is then a stale description of the
// playlist, and writing it ties two tracks to one position or leaves the
// removed one's slot empty: the caller gets a 400 it can retry from instead.
func TestPlaylistMembershipService_Reorder_RefusesAPlanInvalidatedAfterTheRead(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	plRepo := catalogtest.NewPlaylistRepo()
	pl := seedPlaylist(t, plRepo, userId, "My Playlist")
	first, second, third := domain.NewTrackId(), domain.NewTrackId(), domain.NewTrackId()
	pl.Tracks = []domain.PlaylistTrack{
		{TrackId: first, Position: 0},
		{TrackId: second, Position: 1},
		{TrackId: third, Position: 2},
	}
	plRepo.Seed(pl)
	racing := &playlistEditedAfterRead{PlaylistRepo: plRepo, added: domain.NewTrackId()}
	svc := NewPlaylistMembershipService(racing, catalogtest.NewTrackRepo())

	err := svc.Reorder(ctx, userId, pl.ID, []domain.TrackId{third, second, first})

	if !errors.Is(err, ports.ErrPlaylistChangedDuringReorder) {
		t.Fatalf("error = %v, want %v", err, ports.ErrPlaylistChangedDuringReorder)
	}
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.HTTPStatus() != 400 {
		t.Fatalf("error = %v, want a 400 the client can retry from", err)
	}
}

// ownerBlindReadRepo simulates a regressed service-layer ownership check: its
// Exists and GetTrackOrder answer for any playlist regardless of owner, so the
// service's reads no longer stop a foreign caller. The embedded fake's writes
// stay owner-scoped, standing in for the owner-scoped SQL underneath.
type ownerBlindReadRepo struct {
	*catalogtest.PlaylistRepo
}

func (r ownerBlindReadRepo) Exists(_ context.Context, id domain.PlaylistId, _ shared.UserId) (bool, error) {
	_, ok := r.Playlists[id.String()]
	return ok, nil
}

func (r ownerBlindReadRepo) GetTrackOrder(_ context.Context, id domain.PlaylistId, _ shared.UserId) ([]domain.TrackId, bool, error) {
	p, ok := r.Playlists[id.String()]
	if !ok {
		return nil, false, nil
	}
	ids := make([]domain.TrackId, len(p.Tracks))
	for i, t := range p.Tracks {
		ids[i] = t.TrackId
	}
	return ids, true, nil
}

type recordingPublisher struct{ types []string }

func (p *recordingPublisher) Publish(_ context.Context, _ shared.UserId, eventType string, _ map[string]any) {
	p.types = append(p.types, eventType)
}

// TestPlaylistMembershipService_ForeignWriteRefusedBelowLoadCheck proves the
// defense in depth from issue #1044: even when the read-side ownership check is
// bypassed, every membership write passes the caller's userId down, the
// owner-scoped repository refuses it, and the service answers
// ErrPlaylistNotFound without persisting anything or publishing an event.
func TestPlaylistMembershipService_ForeignWriteRefusedBelowLoadCheck(t *testing.T) {
	ctx := context.Background()
	victim := testUserId()
	attacker := shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))

	cases := []struct {
		name string
		call func(*PlaylistMembershipService, domain.PlaylistId, []domain.TrackId, domain.TrackId) error
	}{
		{"AddTrack", func(s *PlaylistMembershipService, pl domain.PlaylistId, _ []domain.TrackId, own domain.TrackId) error {
			return s.AddTrack(ctx, attacker, pl, own)
		}},
		{"AddTracks", func(s *PlaylistMembershipService, pl domain.PlaylistId, _ []domain.TrackId, own domain.TrackId) error {
			_, err := s.AddTracks(ctx, attacker, pl, []domain.TrackId{own})
			return err
		}},
		{"RemoveTrack", func(s *PlaylistMembershipService, pl domain.PlaylistId, members []domain.TrackId, _ domain.TrackId) error {
			return s.RemoveTrack(ctx, attacker, pl, members[0])
		}},
		{"RemoveTracks", func(s *PlaylistMembershipService, pl domain.PlaylistId, members []domain.TrackId, _ domain.TrackId) error {
			_, err := s.RemoveTracks(ctx, attacker, pl, members)
			return err
		}},
		{"Reorder", func(s *PlaylistMembershipService, pl domain.PlaylistId, members []domain.TrackId, _ domain.TrackId) error {
			return s.Reorder(ctx, attacker, pl, []domain.TrackId{members[1], members[0]})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			pl := seedPlaylist(t, plRepo, victim, "Victim's Playlist")
			members := []domain.TrackId{domain.NewTrackId(), domain.NewTrackId()}
			for _, id := range members {
				if err := pl.AddTrack(id, time.Now()); err != nil {
					t.Fatalf("seed AddTrack: %v", err)
				}
			}
			own := seedTrack(t, trRepo, attacker, "Attacker Track", "Artist", "Album")
			pub := &recordingPublisher{}
			svc := NewPlaylistMembershipService(ownerBlindReadRepo{plRepo}, trRepo, WithPlaylistMembershipEvents(pub))

			err := tc.call(svc, pl.ID, members, own.ID)

			if !errors.Is(err, ErrPlaylistNotFound) {
				t.Fatalf("%s by non-owner: err = %v, want ErrPlaylistNotFound", tc.name, err)
			}
			if len(plRepo.Added) != 0 || len(plRepo.Removed) != 0 {
				t.Fatalf("%s by non-owner persisted: added=%v removed=%v", tc.name, plRepo.Added, plRepo.Removed)
			}
			if len(pub.types) != 0 {
				t.Fatalf("%s by non-owner published events: %v", tc.name, pub.types)
			}
		})
	}
}

// TestPlaylistMembershipService_RemoveTrack_LogsActorAndObject pins #1052.
func TestPlaylistMembershipService_RemoveTrack_LogsActorAndObject(t *testing.T) {
	logs := captureAuditLogs(t)
	userId := testUserId()
	plRepo := catalogtest.NewPlaylistRepo()
	pl := seedPlaylist(t, plRepo, userId, "Mix")
	trackId := domain.NewTrackId()
	if err := pl.AddTrack(trackId, time.Now()); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo())
	if err := svc.RemoveTrack(context.Background(), userId, pl.ID, trackId); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertAttrs(t, logs.find(t, "track removed from playlist"), map[string]string{
		"user_id":     userId.String(),
		"playlist_id": pl.ID.String(),
		"track_id":    trackId.String(),
	})
}

// TestPlaylistMembershipService_RemoveTracks_LogsActorAndObject pins #1052:
// the batch line names every track actually removed, not the ones requested.
func TestPlaylistMembershipService_RemoveTracks_LogsActorAndObject(t *testing.T) {
	logs := captureAuditLogs(t)
	userId := testUserId()
	plRepo := catalogtest.NewPlaylistRepo()
	pl := seedPlaylist(t, plRepo, userId, "Mix")
	a, b := domain.NewTrackId(), domain.NewTrackId()
	for _, id := range []domain.TrackId{a, b} {
		if err := pl.AddTrack(id, time.Now()); err != nil {
			t.Fatalf("seed track: %v", err)
		}
	}
	absent := domain.NewTrackId()

	svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo())
	n, err := svc.RemoveTracks(context.Background(), userId, pl.ID, []domain.TrackId{a, absent, b})
	if err != nil || n != 2 {
		t.Fatalf("RemoveTracks = %d, %v; want 2, nil", n, err)
	}

	rec := logs.find(t, "tracks removed from playlist")
	assertAttrs(t, rec, map[string]string{
		"user_id":     userId.String(),
		"playlist_id": pl.ID.String(),
		"removed":     "2",
		"requested":   "3",
	})
	ids := rec.attrs["track_ids"]
	if !strings.Contains(ids, a.String()) || !strings.Contains(ids, b.String()) || strings.Contains(ids, absent.String()) {
		t.Errorf("track_ids = %q, want exactly the removed ids %s and %s", ids, a, b)
	}
}

type recordingPlaylistPublisher struct {
	mu     sync.Mutex
	events []struct {
		typ     string
		payload map[string]any
	}
}

func (p *recordingPlaylistPublisher) Publish(_ context.Context, _ shared.UserId, eventType string, payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, struct {
		typ     string
		payload map[string]any
	}{eventType, payload})
}

func (p *recordingPlaylistPublisher) last(typ string) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.events) - 1; i >= 0; i-- {
		if p.events[i].typ == typ {
			return p.events[i].payload
		}
	}
	return nil
}

func TestPlaylistService_PublishesMutationEvents(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Run("rename", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		pl, _ := domain.NewPlaylist(userId, "Old", time.Now())
		plRepo.Seed(pl)
		svc := NewPlaylistLifecycleService(plRepo, WithPlaylistLifecycleEvents(pub))

		if _, _, err := svc.Rename(ctx, userId, pl.ID, "New Name"); err != nil {
			t.Fatalf("rename: %v", err)
		}
		p := pub.last("playlist_renamed")
		if p == nil || p["playlist_id"] != pl.ID.String() || p["name"] != "New Name" {
			t.Fatalf("playlist_renamed payload = %v", p)
		}
	})

	t.Run("remove track", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		track, _ := domain.NewTrack(userId, "T", "A", "")
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		_ = pl.AddTrack(track.ID, time.Now())
		plRepo.SeedWithTracks(pl, []*domain.Track{track})
		svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo(), WithPlaylistMembershipEvents(pub))

		if err := svc.RemoveTrack(ctx, userId, pl.ID, track.ID); err != nil {
			t.Fatalf("remove track: %v", err)
		}
		p := pub.last("track_removed_from_playlist")
		if p == nil || p["playlist_id"] != pl.ID.String() || p["track_id"] != track.ID.String() {
			t.Fatalf("track_removed_from_playlist payload = %v", p)
		}
	})

	t.Run("reorder", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		t1, _ := domain.NewTrack(userId, "T1", "A", "")
		t2, _ := domain.NewTrack(userId, "T2", "A", "")
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		_ = pl.AddTrack(t1.ID, time.Now())
		_ = pl.AddTrack(t2.ID, time.Now())
		plRepo.SeedWithTracks(pl, []*domain.Track{t1, t2})
		svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo(), WithPlaylistMembershipEvents(pub))

		if err := svc.Reorder(ctx, userId, pl.ID, []domain.TrackId{t2.ID, t1.ID}); err != nil {
			t.Fatalf("reorder: %v", err)
		}
		p := pub.last("playlist_reordered")
		if p == nil || p["playlist_id"] != pl.ID.String() {
			t.Fatalf("playlist_reordered payload = %v", p)
		}
		ids, ok := p["track_ids"].([]string)
		if !ok || len(ids) != 2 || ids[0] != t2.ID.String() || ids[1] != t1.ID.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], t2.ID.String(), t1.ID.String())
		}
	})

	// The membership events below drive mobile optimistic rollback, so their
	// payloads must name exactly the tracks the write changed, in request order.
	t.Run("add tracks names only the inserted tracks in request order", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		member := seedTrack(t, trRepo, userId, "Member", "A", "")
		b := seedTrack(t, trRepo, userId, "B", "A", "")
		a := seedTrack(t, trRepo, userId, "A", "A", "")
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		_ = pl.AddTrack(member.ID, time.Now())
		plRepo.Seed(pl)
		svc := NewPlaylistMembershipService(plRepo, trRepo, WithPlaylistMembershipEvents(pub))

		if _, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{b.ID, member.ID, a.ID, b.ID}); err != nil {
			t.Fatalf("add tracks: %v", err)
		}
		p := pub.last("tracks_added_to_playlist")
		ids, ok := p["track_ids"].([]string)
		if !ok || len(ids) != 2 || ids[0] != b.ID.String() || ids[1] != a.ID.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], b.ID.String(), a.ID.String())
		}
	})

	t.Run("remove tracks names only the removed tracks in request order", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		first, second, third := domain.NewTrackId(), domain.NewTrackId(), domain.NewTrackId()
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		for _, id := range []domain.TrackId{first, second, third} {
			_ = pl.AddTrack(id, time.Now())
		}
		plRepo.Seed(pl)
		svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo(), WithPlaylistMembershipEvents(pub))

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{third, domain.NewTrackId(), first, third}); err != nil {
			t.Fatalf("remove tracks: %v", err)
		}
		p := pub.last("tracks_removed_from_playlist")
		ids, ok := p["track_ids"].([]string)
		if !ok || len(ids) != 2 || ids[0] != third.String() || ids[1] != first.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], third.String(), first.String())
		}
	})

	t.Run("no-op membership writes publish nothing", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		member := seedTrack(t, trRepo, userId, "Member", "A", "")
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		_ = pl.AddTrack(member.ID, time.Now())
		plRepo.Seed(pl)
		svc := NewPlaylistMembershipService(plRepo, trRepo, WithPlaylistMembershipEvents(pub))

		_ = svc.AddTrack(ctx, userId, pl.ID, member.ID)
		_, _ = svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{member.ID})
		_ = svc.RemoveTrack(ctx, userId, pl.ID, domain.NewTrackId())
		_, _ = svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{domain.NewTrackId()})

		if len(pub.events) != 0 {
			t.Fatalf("no-op writes published %v", pub.events)
		}
	})
}
