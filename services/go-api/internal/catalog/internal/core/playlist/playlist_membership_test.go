package playlist_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/shared/events"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const playlistBatchCap = 500

func TestPlaylistMembershipService_AddTrack(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalog.InMemoryPlaylistRepo, *catalog.InMemoryTrackRepo) (catalog.PlaylistId, catalog.TrackId)
		wantErr error
	}{
		{
			name: "track added to playlist",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo, trRepo *catalog.InMemoryTrackRepo) (catalog.PlaylistId, catalog.TrackId) {
				pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
				track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				return pl.ID, track.ID
			},
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo, trRepo *catalog.InMemoryTrackRepo) (catalog.PlaylistId, catalog.TrackId) {
				track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				return catalog.NewPlaylistId(), track.ID
			},
			wantErr: catalog.ErrPlaylistNotFound,
		},
		{
			name: "track not found returns ErrTrackNotFound",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo, trRepo *catalog.InMemoryTrackRepo) (catalog.PlaylistId, catalog.TrackId) {
				pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
				return pl.ID, catalog.NewTrackId()
			},
			wantErr: catalog.ErrTrackNotFound,
		},
		{
			name: "track deleted before the insert returns ErrTrackNotFound",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo, trRepo *catalog.InMemoryTrackRepo) (catalog.PlaylistId, catalog.TrackId) {
				pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
				track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				plRepo.ErrOnAddTrack = catalog.ErrTrackMissing
				return pl.ID, track.ID
			},
			wantErr: catalog.ErrTrackNotFound,
		},
		{
			name: "track already in playlist returns ErrTrackAlreadyInPlaylist",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo, trRepo *catalog.InMemoryTrackRepo) (catalog.PlaylistId, catalog.TrackId) {
				pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
				track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				_ = pl.AddTrack(track.ID, time.Now())
				return pl.ID, track.ID
			},
			wantErr: catalog.ErrTrackAlreadyInPlaylist,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo, trRepo *catalog.InMemoryTrackRepo) (catalog.PlaylistId, catalog.TrackId) {
				plRepo.ErrOnExists = errRepo
				return catalog.NewPlaylistId(), catalog.NewTrackId()
			},
			wantErr: errRepo,
		},
		{
			name: "never loads the playlist's track list",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo, trRepo *catalog.InMemoryTrackRepo) (catalog.PlaylistId, catalog.TrackId) {
				pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
				track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				plRepo.ErrOnGetWithTracks = errRepo
				plRepo.ErrOnGetTrackOrder = errRepo
				return pl.ID, track.ID
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			trRepo := catalog.NewInMemoryTrackRepo()
			playlistId, trackId := tt.setup(plRepo, trRepo)
			svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

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
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	t.Run("adds every owned track at contiguous positions", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		first := catalog.SeedTrack(t, trRepo, userId, "First", "Artist", "Album")
		second := catalog.SeedTrack(t, trRepo, userId, "Second", "Artist", "Album")
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{first.ID, second.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 2 {
			t.Fatalf("added = %d, want 2", added)
		}
		want := []catalog.PlaylistTrack{
			{TrackId: first.ID, Position: 0},
			{TrackId: second.ID, Position: 1},
		}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("appends after the tracks already in the playlist", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		existing := catalog.SeedTrack(t, trRepo, userId, "Existing", "Artist", "Album")
		fresh := catalog.SeedTrack(t, trRepo, userId, "Fresh", "Artist", "Album")
		if err := pl.AddTrack(existing.ID, time.Now()); err != nil {
			t.Fatalf("seed AddTrack: %v", err)
		}
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{fresh.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		want := []catalog.PlaylistTrack{{TrackId: fresh.ID, Position: 1}}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("skips tracks already in the playlist instead of failing the batch", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		existing := catalog.SeedTrack(t, trRepo, userId, "Existing", "Artist", "Album")
		fresh := catalog.SeedTrack(t, trRepo, userId, "Fresh", "Artist", "Album")
		if err := pl.AddTrack(existing.ID, time.Now()); err != nil {
			t.Fatalf("seed AddTrack: %v", err)
		}
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{existing.ID, fresh.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		want := []catalog.PlaylistTrack{{TrackId: fresh.ID, Position: 1}}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("skips a duplicate inside one request", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := catalog.SeedTrack(t, trRepo, userId, "Repeated", "Artist", "Album")
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{track.ID, track.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
	})

	t.Run("skips a track the user does not own", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		owned := catalog.SeedTrack(t, trRepo, userId, "Owned", "Artist", "Album")
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{owned.ID, catalog.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		want := []catalog.PlaylistTrack{{TrackId: owned.ID, Position: 0}}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("does not touch the repository when nothing is addable", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		plRepo.ErrOnAddTracks = errRepo
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{catalog.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 0 {
			t.Fatalf("added = %d, want 0", added)
		}
	})

	t.Run("takes a batch at the cap and rejects one past it", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		svc := catalog.NewPlaylistMembershipService(plRepo, catalog.NewInMemoryTrackRepo())

		assertBatchCap(t, func(ids []catalog.TrackId) error {
			_, err := svc.AddTracks(ctx, userId, pl.ID, ids)
			return err
		})
	})

	t.Run("playlist not found returns ErrPlaylistNotFound", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.AddTracks(ctx, userId, catalog.NewPlaylistId(), []catalog.TrackId{catalog.NewTrackId()})

		if !errors.Is(err, catalog.ErrPlaylistNotFound) {
			t.Fatalf("error = %v, want %v", err, catalog.ErrPlaylistNotFound)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTracks = errRepo
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{track.ID})

		if !errors.Is(err, errRepo) {
			t.Fatalf("error = %v, want %v", err, errRepo)
		}
	})

	t.Run("a track deleted before the insert returns ErrTrackNotFound", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTracks = catalog.ErrTrackMissing
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{track.ID})

		if !errors.Is(err, catalog.ErrTrackNotFound) {
			t.Fatalf("error = %v, want %v", err, catalog.ErrTrackNotFound)
		}
	})
}

func freshTrackIds(n int) []catalog.TrackId {
	ids := make([]catalog.TrackId, n)
	for i := range ids {
		ids[i] = catalog.NewTrackId()
	}
	return ids
}

func httpStatus(err error) int {
	var coded interface{ HTTPStatus() int }
	if !errors.As(err, &coded) {
		return 0
	}
	return coded.HTTPStatus()
}

func assertBatchCap(t *testing.T, call func([]catalog.TrackId) error) {
	t.Helper()
	if err := call(freshTrackIds(playlistBatchCap)); err != nil {
		t.Fatalf("a batch of exactly %d: %v, want it taken", playlistBatchCap, err)
	}
	if err := call(freshTrackIds(playlistBatchCap + 1)); httpStatus(err) != 400 {
		t.Fatalf("a batch of %d: error = %v, want a 400 validation error", playlistBatchCap+1, err)
	}
}

func assertPlaylistFull(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, catalog.ErrPlaylistFull) {
		t.Fatalf("error = %v, want ErrPlaylistFull", err)
	}
	if err.Error() != catalog.ErrPlaylistFull.Error() {
		t.Fatalf("detail = %q, want %q", err.Error(), catalog.ErrPlaylistFull.Error())
	}
}

func TestPlaylistMembershipService_AddPastTheCap_SurfacesTheRefusalWhole(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()

	t.Run("AddTrack", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTrack = catalog.ErrPlaylistFull
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		err := svc.AddTrack(ctx, userId, pl.ID, track.ID)

		assertPlaylistFull(t, err)
	})

	t.Run("AddTracks", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTracks = catalog.ErrPlaylistFull
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{track.ID})

		assertPlaylistFull(t, err)
	})
}

func TestPlaylistMembershipService_AddTrack_SurfacesTheDuplicateRefusalWhole(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	plRepo := catalog.NewInMemoryPlaylistRepo()
	trRepo := catalog.NewInMemoryTrackRepo()
	pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
	track := catalog.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
	if err := pl.AddTrack(track.ID, time.Now()); err != nil {
		t.Fatalf("seed AddTrack: %v", err)
	}
	svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

	err := svc.AddTrack(ctx, userId, pl.ID, track.ID)

	if !errors.Is(err, catalog.ErrTrackAlreadyInPlaylist) || err.Error() != catalog.ErrTrackAlreadyInPlaylist.Error() {
		t.Fatalf("error = %v, want the bare ErrTrackAlreadyInPlaylist", err)
	}
	if httpStatus(err) != 409 {
		t.Fatalf("status = %d, want 409", httpStatus(err))
	}
}

type addTracksRecorder struct {
	*catalog.InMemoryPlaylistRepo
	asked [][]catalog.TrackId
}

func (r *addTracksRecorder) AddTracks(ctx context.Context, userId shared.UserId, playlistId catalog.PlaylistId, trackIds []catalog.TrackId) ([]catalog.TrackId, error) {
	r.asked = append(r.asked, append([]catalog.TrackId(nil), trackIds...))
	return r.InMemoryPlaylistRepo.AddTracks(ctx, userId, playlistId, trackIds)
}

func TestPlaylistMembershipService_AddTracks_SendsEachRepeatedTrackOnce(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	plRepo := &addTracksRecorder{InMemoryPlaylistRepo: catalog.NewInMemoryPlaylistRepo()}
	trRepo := catalog.NewInMemoryTrackRepo()
	pl := catalog.SeedPlaylist(t, plRepo.InMemoryPlaylistRepo, userId, "My Playlist")
	a := catalog.SeedTrack(t, trRepo, userId, "A", "Artist", "Album")
	b := catalog.SeedTrack(t, trRepo, userId, "B", "Artist", "Album")
	svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

	added, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{a.ID, b.ID, a.ID, b.ID})
	if err != nil {
		t.Fatalf("AddTracks: %v", err)
	}
	if added != 2 {
		t.Fatalf("added = %d, want 2", added)
	}
	want := [][]catalog.TrackId{{a.ID, b.ID}}
	if !reflect.DeepEqual(plRepo.asked, want) {
		t.Fatalf("repository asked to add %v, want %v: each track once, in request order", plRepo.asked, want)
	}
}

func TestPlaylistMembershipService_RemoveTrack(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalog.InMemoryPlaylistRepo) (catalog.PlaylistId, catalog.TrackId)
		wantErr error
	}{
		{
			name: "track removed from playlist",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo) (catalog.PlaylistId, catalog.TrackId) {
				pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
				trackId := catalog.NewTrackId()
				return pl.ID, trackId
			},
			wantErr: nil,
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo) (catalog.PlaylistId, catalog.TrackId) {
				return catalog.NewPlaylistId(), catalog.NewTrackId()
			},
			wantErr: catalog.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo) (catalog.PlaylistId, catalog.TrackId) {
				plRepo.ErrOnRemoveTrack = errRepo
				return catalog.NewPlaylistId(), catalog.NewTrackId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			trRepo := catalog.NewInMemoryTrackRepo()
			playlistId, trackId := tt.setup(plRepo)
			svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

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
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	seedPlaylistWithTracks := func(t *testing.T, plRepo *catalog.InMemoryPlaylistRepo, trackIds ...catalog.TrackId) *catalog.Playlist {
		t.Helper()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		for _, id := range trackIds {
			if err := pl.AddTrack(id, time.Now()); err != nil {
				t.Fatalf("seed AddTrack: %v", err)
			}
		}
		return pl
	}

	t.Run("removes every listed track in one repository call", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		first, second, third := catalog.NewTrackId(), catalog.NewTrackId(), catalog.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, first, second, third)
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []catalog.TrackId{first, third})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 2 {
			t.Fatalf("removed = %d, want 2", removed)
		}
		if !reflect.DeepEqual(plRepo.Removed, []catalog.TrackId{first, third}) {
			t.Fatalf("persisted = %v, want %v", plRepo.Removed, []catalog.TrackId{first, third})
		}
	})

	t.Run("leaves the surviving tracks at contiguous positions", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		first, second, third := catalog.NewTrackId(), catalog.NewTrackId(), catalog.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, first, second, third)
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []catalog.TrackId{first}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []catalog.PlaylistTrack{
			{TrackId: second, Position: 0},
			{TrackId: third, Position: 1},
		}
		if !reflect.DeepEqual(pl.Tracks, want) {
			t.Fatalf("tracks = %v, want %v", pl.Tracks, want)
		}
	})

	t.Run("skips a track that is not in the playlist rather than failing the batch", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		member := catalog.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, member)
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []catalog.TrackId{member, catalog.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 1 {
			t.Fatalf("removed = %d, want 1", removed)
		}
	})

	t.Run("removes nothing and reports zero when nothing was a member", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		member := catalog.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, member)
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []catalog.TrackId{catalog.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 0 || len(plRepo.Removed) != 0 {
			t.Fatalf("removed = %d (persisted %v), want 0", removed, plRepo.Removed)
		}
		if want := []catalog.PlaylistTrack{{TrackId: member, Position: 0}}; !reflect.DeepEqual(pl.Tracks, want) {
			t.Fatalf("tracks = %v, want %v", pl.Tracks, want)
		}
	})

	t.Run("never loads the playlist's track list", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		first, second := catalog.NewTrackId(), catalog.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, first, second)
		plRepo.ErrOnGetWithTracks = errRepo
		plRepo.ErrOnGetTrackOrder = errRepo
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []catalog.TrackId{first}); err != nil {
			t.Fatalf("RemoveTracks: %v", err)
		}
		if err := svc.RemoveTrack(ctx, userId, pl.ID, second); err != nil {
			t.Fatalf("RemoveTrack: %v", err)
		}
		if len(pl.Tracks) != 0 {
			t.Fatalf("tracks = %v, want none", pl.Tracks)
		}
	})

	t.Run("takes a batch at the cap and rejects one past it", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
		svc := catalog.NewPlaylistMembershipService(plRepo, catalog.NewInMemoryTrackRepo())

		assertBatchCap(t, func(ids []catalog.TrackId) error {
			_, err := svc.RemoveTracks(ctx, userId, pl.ID, ids)
			return err
		})
	})

	t.Run("playlist not found returns ErrPlaylistNotFound", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.RemoveTracks(ctx, userId, catalog.NewPlaylistId(), []catalog.TrackId{catalog.NewTrackId()})

		if !errors.Is(err, catalog.ErrPlaylistNotFound) {
			t.Fatalf("error = %v, want %v", err, catalog.ErrPlaylistNotFound)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		member := catalog.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, member)
		plRepo.ErrOnRemoveTracks = errRepo
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

		_, err := svc.RemoveTracks(ctx, userId, pl.ID, []catalog.TrackId{member})

		if !errors.Is(err, errRepo) {
			t.Fatalf("error = %v, want %v", err, errRepo)
		}
	})
}

func TestPlaylistMembershipService_Reorder(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalog.InMemoryPlaylistRepo) (catalog.PlaylistId, []catalog.TrackId)
		wantErr error
	}{
		{
			name: "valid reorder succeeds",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo) (catalog.PlaylistId, []catalog.TrackId) {
				pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
				t1 := catalog.NewTrackId()
				t2 := catalog.NewTrackId()
				pl.Tracks = []catalog.PlaylistTrack{
					{TrackId: t1, Position: 0},
					{TrackId: t2, Position: 1},
				}
				plRepo.Seed(pl)
				return pl.ID, []catalog.TrackId{t2, t1}
			},
			wantErr: nil,
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo) (catalog.PlaylistId, []catalog.TrackId) {
				return catalog.NewPlaylistId(), []catalog.TrackId{}
			},
			wantErr: catalog.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *catalog.InMemoryPlaylistRepo) (catalog.PlaylistId, []catalog.TrackId) {
				plRepo.ErrOnGetTrackOrder = errRepo
				return catalog.NewPlaylistId(), []catalog.TrackId{}
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			trRepo := catalog.NewInMemoryTrackRepo()
			playlistId, trackIds := tt.setup(plRepo)
			svc := catalog.NewPlaylistMembershipService(plRepo, trRepo)

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

func TestPlaylistMembershipService_Reorder_RefusesAnOrderThatIsNotThePlaylist(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	first, second := catalog.NewTrackId(), catalog.NewTrackId()

	cases := []struct {
		name  string
		order []catalog.TrackId
	}{
		{"a member is missing", []catalog.TrackId{second}},
		{"an extra track is listed", []catalog.TrackId{second, first, catalog.NewTrackId()}},
		{"a stranger replaces a member", []catalog.TrackId{second, catalog.NewTrackId()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
			for _, id := range []catalog.TrackId{first, second} {
				if err := pl.AddTrack(id, time.Now()); err != nil {
					t.Fatalf("seed AddTrack: %v", err)
				}
			}
			svc := catalog.NewPlaylistMembershipService(plRepo, catalog.NewInMemoryTrackRepo())

			err := svc.Reorder(ctx, userId, pl.ID, tc.order)

			if httpStatus(err) != 400 {
				t.Fatalf("error = %v, want a 400 validation error", err)
			}
			want := []catalog.PlaylistTrack{{TrackId: first, Position: 0}, {TrackId: second, Position: 1}}
			if !reflect.DeepEqual(pl.Tracks, want) {
				t.Fatalf("tracks = %v, want the order left as %v", pl.Tracks, want)
			}
		})
	}
}

func TestPlaylistMembershipService_Reorder_AnUnknownPlaylistIsNotFoundWhateverTheOrder(t *testing.T) {
	svc := catalog.NewPlaylistMembershipService(catalog.NewInMemoryPlaylistRepo(), catalog.NewInMemoryTrackRepo())

	err := svc.Reorder(context.Background(), catalog.FixtureUserId(), catalog.NewPlaylistId(), []catalog.TrackId{catalog.NewTrackId()})

	if !errors.Is(err, catalog.ErrPlaylistNotFound) {
		t.Fatalf("error = %v, want ErrPlaylistNotFound rather than a complaint about the order", err)
	}
}

type playlistEditedAfterRead struct {
	*catalog.InMemoryPlaylistRepo
	added catalog.TrackId
}

func (r *playlistEditedAfterRead) GetTrackOrder(ctx context.Context, playlistId catalog.PlaylistId, userId shared.UserId) ([]catalog.TrackId, bool, error) {
	ids, found, err := r.InMemoryPlaylistRepo.GetTrackOrder(ctx, playlistId, userId)
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

func TestPlaylistMembershipService_Reorder_RefusesAPlanInvalidatedAfterTheRead(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	plRepo := catalog.NewInMemoryPlaylistRepo()
	pl := catalog.SeedPlaylist(t, plRepo, userId, "My Playlist")
	first, second, third := catalog.NewTrackId(), catalog.NewTrackId(), catalog.NewTrackId()
	pl.Tracks = []catalog.PlaylistTrack{
		{TrackId: first, Position: 0},
		{TrackId: second, Position: 1},
		{TrackId: third, Position: 2},
	}
	plRepo.Seed(pl)
	racing := &playlistEditedAfterRead{InMemoryPlaylistRepo: plRepo, added: catalog.NewTrackId()}
	svc := catalog.NewPlaylistMembershipService(racing, catalog.NewInMemoryTrackRepo())

	err := svc.Reorder(ctx, userId, pl.ID, []catalog.TrackId{third, second, first})

	if !errors.Is(err, catalog.ErrPlaylistChangedDuringReorder) {
		t.Fatalf("error = %v, want %v", err, catalog.ErrPlaylistChangedDuringReorder)
	}
	if httpStatus(err) != 400 {
		t.Fatalf("error = %v, want a 400 the client can retry from", err)
	}
}

type ownerBlindReadRepo struct {
	*catalog.InMemoryPlaylistRepo
}

func (r ownerBlindReadRepo) Exists(_ context.Context, id catalog.PlaylistId, _ shared.UserId) (bool, error) {
	_, ok := r.Playlists[id.String()]
	return ok, nil
}

func (r ownerBlindReadRepo) GetTrackOrder(_ context.Context, id catalog.PlaylistId, _ shared.UserId) ([]catalog.TrackId, bool, error) {
	p, ok := r.Playlists[id.String()]
	if !ok {
		return nil, false, nil
	}
	ids := make([]catalog.TrackId, len(p.Tracks))
	for i, t := range p.Tracks {
		ids[i] = t.TrackId
	}
	return ids, true, nil
}

type recordingPublisher struct{ types []string }

func (p *recordingPublisher) Publish(_ context.Context, _ shared.UserId, eventType events.EventType, _ map[string]any) {
	p.types = append(p.types, string(eventType))
}

func TestPlaylistMembershipService_ForeignWriteRefusedBelowLoadCheck(t *testing.T) {
	ctx := context.Background()
	victim := catalog.FixtureUserId()
	attacker := shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))

	cases := []struct {
		name string
		call func(*catalog.PlaylistMembershipService, catalog.PlaylistId, []catalog.TrackId, catalog.TrackId) error
	}{
		{"AddTrack", func(s *catalog.PlaylistMembershipService, pl catalog.PlaylistId, _ []catalog.TrackId, own catalog.TrackId) error {
			return s.AddTrack(ctx, attacker, pl, own)
		}},
		{"AddTracks", func(s *catalog.PlaylistMembershipService, pl catalog.PlaylistId, _ []catalog.TrackId, own catalog.TrackId) error {
			_, err := s.AddTracks(ctx, attacker, pl, []catalog.TrackId{own})
			return err
		}},
		{"RemoveTrack", func(s *catalog.PlaylistMembershipService, pl catalog.PlaylistId, members []catalog.TrackId, _ catalog.TrackId) error {
			return s.RemoveTrack(ctx, attacker, pl, members[0])
		}},
		{"RemoveTracks", func(s *catalog.PlaylistMembershipService, pl catalog.PlaylistId, members []catalog.TrackId, _ catalog.TrackId) error {
			_, err := s.RemoveTracks(ctx, attacker, pl, members)
			return err
		}},
		{"Reorder", func(s *catalog.PlaylistMembershipService, pl catalog.PlaylistId, members []catalog.TrackId, _ catalog.TrackId) error {
			return s.Reorder(ctx, attacker, pl, []catalog.TrackId{members[1], members[0]})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			trRepo := catalog.NewInMemoryTrackRepo()
			pl := catalog.SeedPlaylist(t, plRepo, victim, "Victim's Playlist")
			members := []catalog.TrackId{catalog.NewTrackId(), catalog.NewTrackId()}
			for _, id := range members {
				if err := pl.AddTrack(id, time.Now()); err != nil {
					t.Fatalf("seed AddTrack: %v", err)
				}
			}
			own := catalog.SeedTrack(t, trRepo, attacker, "Attacker Track", "Artist", "Album")
			pub := &recordingPublisher{}
			svc := catalog.NewPlaylistMembershipService(ownerBlindReadRepo{plRepo}, trRepo, catalog.WithPlaylistMembershipEvents(pub))

			err := tc.call(svc, pl.ID, members, own.ID)

			if !errors.Is(err, catalog.ErrPlaylistNotFound) {
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

func TestPlaylistMembershipService_RemoveTrack_LogsActorAndObject(t *testing.T) {
	logs := catalog.CaptureAuditLogs(t)
	userId := catalog.FixtureUserId()
	plRepo := catalog.NewInMemoryPlaylistRepo()
	pl := catalog.SeedPlaylist(t, plRepo, userId, "Mix")
	trackId := catalog.NewTrackId()
	if err := pl.AddTrack(trackId, time.Now()); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	svc := catalog.NewPlaylistMembershipService(plRepo, catalog.NewInMemoryTrackRepo())
	if err := svc.RemoveTrack(context.Background(), userId, pl.ID, trackId); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	catalog.AssertAttrs(t, logs.Find(t, "track removed from playlist"), map[string]string{
		"user_id":     userId.String(),
		"playlist_id": pl.ID.String(),
		"track_id":    trackId.String(),
	})
}

func TestPlaylistMembershipService_RemoveTracks_LogsActorAndObject(t *testing.T) {
	logs := catalog.CaptureAuditLogs(t)
	userId := catalog.FixtureUserId()
	plRepo := catalog.NewInMemoryPlaylistRepo()
	pl := catalog.SeedPlaylist(t, plRepo, userId, "Mix")
	a, b := catalog.NewTrackId(), catalog.NewTrackId()
	for _, id := range []catalog.TrackId{a, b} {
		if err := pl.AddTrack(id, time.Now()); err != nil {
			t.Fatalf("seed track: %v", err)
		}
	}
	absent := catalog.NewTrackId()

	svc := catalog.NewPlaylistMembershipService(plRepo, catalog.NewInMemoryTrackRepo())
	n, err := svc.RemoveTracks(context.Background(), userId, pl.ID, []catalog.TrackId{a, absent, b})
	if err != nil || n != 2 {
		t.Fatalf("RemoveTracks = %d, %v; want 2, nil", n, err)
	}

	rec := logs.Find(t, "tracks removed from playlist")
	catalog.AssertAttrs(t, rec, map[string]string{
		"user_id":     userId.String(),
		"playlist_id": pl.ID.String(),
		"removed":     "2",
		"requested":   "3",
	})
	ids := rec.Attrs["track_ids"]
	if !strings.Contains(ids, a.String()) || !strings.Contains(ids, b.String()) || strings.Contains(ids, absent.String()) {
		t.Errorf("track_ids = %q, want exactly the removed ids %s and %s", ids, a, b)
	}
}

func TestPlaylistMembershipService_PublishesMutationEvents(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Run("remove track", func(t *testing.T) {
		pub := &catalog.InMemoryRecordingPublisher{}
		plRepo := catalog.NewInMemoryPlaylistRepo()
		track, _ := catalog.NewTrack(userId, "T", "A", "")
		pl := catalog.SeedPlaylist(t, plRepo, userId, "PL")
		_ = pl.AddTrack(track.ID, time.Now())
		plRepo.SeedWithTracks(pl, []*catalog.Track{track})
		svc := catalog.NewPlaylistMembershipService(plRepo, catalog.NewInMemoryTrackRepo(), catalog.WithPlaylistMembershipEvents(pub))

		if err := svc.RemoveTrack(ctx, userId, pl.ID, track.ID); err != nil {
			t.Fatalf("remove track: %v", err)
		}
		p := pub.Last("track_removed_from_playlist")
		if p == nil || p["playlist_id"] != pl.ID.String() || p["track_id"] != track.ID.String() {
			t.Fatalf("track_removed_from_playlist payload = %v", p)
		}
	})

	t.Run("reorder", func(t *testing.T) {
		pub := &catalog.InMemoryRecordingPublisher{}
		plRepo := catalog.NewInMemoryPlaylistRepo()
		t1, _ := catalog.NewTrack(userId, "T1", "A", "")
		t2, _ := catalog.NewTrack(userId, "T2", "A", "")
		pl := catalog.SeedPlaylist(t, plRepo, userId, "PL")
		_ = pl.AddTrack(t1.ID, time.Now())
		_ = pl.AddTrack(t2.ID, time.Now())
		plRepo.SeedWithTracks(pl, []*catalog.Track{t1, t2})
		svc := catalog.NewPlaylistMembershipService(plRepo, catalog.NewInMemoryTrackRepo(), catalog.WithPlaylistMembershipEvents(pub))

		if err := svc.Reorder(ctx, userId, pl.ID, []catalog.TrackId{t2.ID, t1.ID}); err != nil {
			t.Fatalf("reorder: %v", err)
		}
		p := pub.Last("playlist_reordered")
		if p == nil || p["playlist_id"] != pl.ID.String() {
			t.Fatalf("playlist_reordered payload = %v", p)
		}
		ids, ok := p["track_ids"].([]string)
		if !ok || len(ids) != 2 || ids[0] != t2.ID.String() || ids[1] != t1.ID.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], t2.ID.String(), t1.ID.String())
		}
	})

	t.Run("add tracks names only the inserted tracks in request order", func(t *testing.T) {
		pub := &catalog.InMemoryRecordingPublisher{}
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		member := catalog.SeedTrack(t, trRepo, userId, "Member", "A", "")
		b := catalog.SeedTrack(t, trRepo, userId, "B", "A", "")
		a := catalog.SeedTrack(t, trRepo, userId, "A", "A", "")
		pl := catalog.SeedPlaylist(t, plRepo, userId, "PL")
		_ = pl.AddTrack(member.ID, time.Now())
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo, catalog.WithPlaylistMembershipEvents(pub))

		if _, err := svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{b.ID, member.ID, a.ID, b.ID}); err != nil {
			t.Fatalf("add tracks: %v", err)
		}
		p := pub.Last("tracks_added_to_playlist")
		ids, ok := p["track_ids"].([]string)
		if !ok || len(ids) != 2 || ids[0] != b.ID.String() || ids[1] != a.ID.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], b.ID.String(), a.ID.String())
		}
	})

	t.Run("remove tracks names only the removed tracks in request order", func(t *testing.T) {
		pub := &catalog.InMemoryRecordingPublisher{}
		plRepo := catalog.NewInMemoryPlaylistRepo()
		first, second, third := catalog.NewTrackId(), catalog.NewTrackId(), catalog.NewTrackId()
		pl := catalog.SeedPlaylist(t, plRepo, userId, "PL")
		for _, id := range []catalog.TrackId{first, second, third} {
			_ = pl.AddTrack(id, time.Now())
		}
		svc := catalog.NewPlaylistMembershipService(plRepo, catalog.NewInMemoryTrackRepo(), catalog.WithPlaylistMembershipEvents(pub))

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []catalog.TrackId{third, catalog.NewTrackId(), first, third}); err != nil {
			t.Fatalf("remove tracks: %v", err)
		}
		p := pub.Last("tracks_removed_from_playlist")
		ids, ok := p["track_ids"].([]string)
		if !ok || len(ids) != 2 || ids[0] != third.String() || ids[1] != first.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], third.String(), first.String())
		}
	})

	t.Run("no-op membership writes publish nothing", func(t *testing.T) {
		pub := &catalog.InMemoryRecordingPublisher{}
		plRepo := catalog.NewInMemoryPlaylistRepo()
		trRepo := catalog.NewInMemoryTrackRepo()
		member := catalog.SeedTrack(t, trRepo, userId, "Member", "A", "")
		pl := catalog.SeedPlaylist(t, plRepo, userId, "PL")
		_ = pl.AddTrack(member.ID, time.Now())
		svc := catalog.NewPlaylistMembershipService(plRepo, trRepo, catalog.WithPlaylistMembershipEvents(pub))

		_ = svc.AddTrack(ctx, userId, pl.ID, member.ID)
		_, _ = svc.AddTracks(ctx, userId, pl.ID, []catalog.TrackId{member.ID})
		_ = svc.RemoveTrack(ctx, userId, pl.ID, catalog.NewTrackId())
		_, _ = svc.RemoveTracks(ctx, userId, pl.ID, []catalog.TrackId{catalog.NewTrackId()})

		if len(pub.Events) != 0 {
			t.Fatalf("no-op writes published %v", pub.Events)
		}
	})
}
