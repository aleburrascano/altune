package playlist_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const playlistBatchCap = 500

func TestNewPlaylistMembershipService_RefusesAMissingCollaborator(t *testing.T) {
	repo := library.NewInMemoryPlaylistRepo()
	tracks := library.NewInMemoryTrackRepo()
	pub := events.NoopPublisher()
	tests := []struct {
		name string
		make func() (*library.PlaylistMembershipService, error)
		want error
	}{
		{"playlist repository", func() (*library.PlaylistMembershipService, error) {
			return library.NewPlaylistMembershipService(nil, tracks, pub, time.Now)
		}, library.ErrPlaylistCollaboratorRequired},
		{"track lookup", func() (*library.PlaylistMembershipService, error) {
			return library.NewPlaylistMembershipService(repo, nil, pub, time.Now)
		}, library.ErrPlaylistCollaboratorRequired},
		{"events", func() (*library.PlaylistMembershipService, error) {
			return library.NewPlaylistMembershipService(repo, tracks, nil, time.Now)
		}, library.ErrPlaylistCollaboratorRequired},
		{"clock", func() (*library.PlaylistMembershipService, error) {
			return library.NewPlaylistMembershipService(repo, tracks, pub, nil)
		}, library.ErrPlaylistClockRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := tt.make()

			if !errors.Is(err, tt.want) || svc != nil {
				t.Fatalf("construction without %s = (%v, %v), want %v and no service", tt.name, svc, err, tt.want)
			}
			if !strings.Contains(err.Error(), "NewPlaylistMembershipService") {
				t.Fatalf("error %q does not name the constructor", err)
			}
		})
	}
}

func TestMustNewPlaylistMembershipService_PanicsOnANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustNewPlaylistMembershipService accepted a nil clock")
		}
	}()

	library.MustNewPlaylistMembershipService(library.NewInMemoryPlaylistRepo(), library.NewInMemoryTrackRepo(), events.NoopPublisher(), nil)
}

func TestPlaylistMembershipService_AddTrack(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*library.InMemoryPlaylistRepo, *library.InMemoryTrackRepo) (library.PlaylistId, library.TrackId)
		wantErr error
	}{
		{
			name: "track added to playlist",
			setup: func(plRepo *library.InMemoryPlaylistRepo, trRepo *library.InMemoryTrackRepo) (library.PlaylistId, library.TrackId) {
				pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
				track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				return pl.ID, track.ID
			},
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *library.InMemoryPlaylistRepo, trRepo *library.InMemoryTrackRepo) (library.PlaylistId, library.TrackId) {
				track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				return librarytest.NewPlaylistId(), track.ID
			},
			wantErr: library.ErrPlaylistNotFound,
		},
		{
			name: "track not found returns ErrTrackNotFound",
			setup: func(plRepo *library.InMemoryPlaylistRepo, trRepo *library.InMemoryTrackRepo) (library.PlaylistId, library.TrackId) {
				pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
				return pl.ID, librarytest.NewTrackId()
			},
			wantErr: library.ErrTrackNotFound,
		},
		{
			name: "track deleted before the insert returns ErrTrackNotFound",
			setup: func(plRepo *library.InMemoryPlaylistRepo, trRepo *library.InMemoryTrackRepo) (library.PlaylistId, library.TrackId) {
				pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
				track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				plRepo.ErrOnAddTrack = library.ErrTrackMissing
				return pl.ID, track.ID
			},
			wantErr: library.ErrTrackNotFound,
		},
		{
			name: "track already in playlist returns ErrTrackAlreadyInPlaylist",
			setup: func(plRepo *library.InMemoryPlaylistRepo, trRepo *library.InMemoryTrackRepo) (library.PlaylistId, library.TrackId) {
				pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
				track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				_ = pl.AddTrack(track.ID, time.Now())
				return pl.ID, track.ID
			},
			wantErr: library.ErrTrackAlreadyInPlaylist,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *library.InMemoryPlaylistRepo, trRepo *library.InMemoryTrackRepo) (library.PlaylistId, library.TrackId) {
				plRepo.ErrOnExists = errRepo
				return librarytest.NewPlaylistId(), librarytest.NewTrackId()
			},
			wantErr: errRepo,
		},
		{
			name: "never loads the playlist's track list",
			setup: func(plRepo *library.InMemoryPlaylistRepo, trRepo *library.InMemoryTrackRepo) (library.PlaylistId, library.TrackId) {
				pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
				track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
				plRepo.ErrOnGetWithTracks = errRepo
				plRepo.ErrOnGetTrackOrder = errRepo
				return pl.ID, track.ID
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := library.NewInMemoryPlaylistRepo()
			trRepo := library.NewInMemoryTrackRepo()
			playlistId, trackId := tt.setup(plRepo, trRepo)
			svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

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
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	t.Run("adds every owned track at contiguous positions", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		first := librarytest.SeedTrack(t, trRepo, userId, "First", "Artist", "Album")
		second := librarytest.SeedTrack(t, trRepo, userId, "Second", "Artist", "Album")
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{first.ID, second.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 2 {
			t.Fatalf("added = %d, want 2", added)
		}
		want := []library.PlaylistTrack{
			{TrackId: first.ID, Position: 0},
			{TrackId: second.ID, Position: 1},
		}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("appends after the tracks already in the playlist", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		existing := librarytest.SeedTrack(t, trRepo, userId, "Existing", "Artist", "Album")
		fresh := librarytest.SeedTrack(t, trRepo, userId, "Fresh", "Artist", "Album")
		if err := pl.AddTrack(existing.ID, time.Now()); err != nil {
			t.Fatalf("seed AddTrack: %v", err)
		}
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{fresh.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		want := []library.PlaylistTrack{{TrackId: fresh.ID, Position: 1}}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("skips tracks already in the playlist instead of failing the batch", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		existing := librarytest.SeedTrack(t, trRepo, userId, "Existing", "Artist", "Album")
		fresh := librarytest.SeedTrack(t, trRepo, userId, "Fresh", "Artist", "Album")
		if err := pl.AddTrack(existing.ID, time.Now()); err != nil {
			t.Fatalf("seed AddTrack: %v", err)
		}
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{existing.ID, fresh.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		want := []library.PlaylistTrack{{TrackId: fresh.ID, Position: 1}}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("skips a duplicate inside one request", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := librarytest.SeedTrack(t, trRepo, userId, "Repeated", "Artist", "Album")
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{track.ID, track.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
	})

	t.Run("skips a track the user does not own", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		owned := librarytest.SeedTrack(t, trRepo, userId, "Owned", "Artist", "Album")
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{owned.ID, librarytest.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 1 {
			t.Fatalf("added = %d, want 1", added)
		}
		want := []library.PlaylistTrack{{TrackId: owned.ID, Position: 0}}
		if !reflect.DeepEqual(plRepo.Added, want) {
			t.Fatalf("persisted = %v, want %v", plRepo.Added, want)
		}
	})

	t.Run("does not touch the repository when nothing is addable", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		plRepo.ErrOnAddTracks = errRepo
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		added, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{librarytest.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 0 {
			t.Fatalf("added = %d, want 0", added)
		}
	})

	t.Run("takes a batch at the cap and rejects one past it", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		svc := library.MustNewPlaylistMembershipService(plRepo, library.NewInMemoryTrackRepo(), events.NoopPublisher(), time.Now)

		assertBatchCap(t, func(ids []library.TrackId) error {
			_, err := svc.AddTracks(ctx, userId, pl.ID, ids)
			return err
		})
	})

	t.Run("playlist not found returns ErrPlaylistNotFound", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		_, err := svc.AddTracks(ctx, userId, librarytest.NewPlaylistId(), []library.TrackId{librarytest.NewTrackId()})

		if !errors.Is(err, library.ErrPlaylistNotFound) {
			t.Fatalf("error = %v, want %v", err, library.ErrPlaylistNotFound)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTracks = errRepo
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		_, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{track.ID})

		if !errors.Is(err, errRepo) {
			t.Fatalf("error = %v, want %v", err, errRepo)
		}
	})

	t.Run("a track deleted before the insert returns ErrTrackNotFound", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTracks = library.ErrTrackMissing
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		_, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{track.ID})

		if !errors.Is(err, library.ErrTrackNotFound) {
			t.Fatalf("error = %v, want %v", err, library.ErrTrackNotFound)
		}
	})
}

func freshTrackIds(n int) []library.TrackId {
	ids := make([]library.TrackId, n)
	for i := range ids {
		ids[i] = librarytest.NewTrackId()
	}
	return ids
}

func httpStatus(err error) int {
	rec := httptest.NewRecorder()
	httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/playlists", nil), err)
	return rec.Code
}

func assertBatchCap(t *testing.T, call func([]library.TrackId) error) {
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
	if !errors.Is(err, library.ErrPlaylistFull) {
		t.Fatalf("error = %v, want ErrPlaylistFull", err)
	}
	if err.Error() != library.ErrPlaylistFull.Error() {
		t.Fatalf("detail = %q, want %q", err.Error(), library.ErrPlaylistFull.Error())
	}
}

func TestPlaylistMembershipService_AddPastTheCap_SurfacesTheRefusalWhole(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()

	t.Run("AddTrack", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTrack = library.ErrPlaylistFull
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		err := svc.AddTrack(ctx, userId, pl.ID, track.ID)

		assertPlaylistFull(t, err)
	})

	t.Run("AddTracks", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
		plRepo.ErrOnAddTracks = library.ErrPlaylistFull
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		_, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{track.ID})

		assertPlaylistFull(t, err)
	})
}

func TestPlaylistMembershipService_AddTrack_SurfacesTheDuplicateRefusalWhole(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	plRepo := library.NewInMemoryPlaylistRepo()
	trRepo := library.NewInMemoryTrackRepo()
	pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
	track := librarytest.SeedTrack(t, trRepo, userId, "Track", "Artist", "Album")
	if err := pl.AddTrack(track.ID, time.Now()); err != nil {
		t.Fatalf("seed AddTrack: %v", err)
	}
	svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

	err := svc.AddTrack(ctx, userId, pl.ID, track.ID)

	if !errors.Is(err, library.ErrTrackAlreadyInPlaylist) || err.Error() != library.ErrTrackAlreadyInPlaylist.Error() {
		t.Fatalf("error = %v, want the bare ErrTrackAlreadyInPlaylist", err)
	}
	if httpStatus(err) != 409 {
		t.Fatalf("status = %d, want 409", httpStatus(err))
	}
}

type addTracksRecorder struct {
	*library.InMemoryPlaylistRepo
	asked [][]library.TrackId
}

func (r *addTracksRecorder) AddTracks(ctx context.Context, userId sharedids.UserId, playlistId library.PlaylistId, trackIds []library.TrackId) ([]library.TrackId, error) {
	r.asked = append(r.asked, append([]library.TrackId(nil), trackIds...))
	return r.InMemoryPlaylistRepo.AddTracks(ctx, userId, playlistId, trackIds)
}

func TestPlaylistMembershipService_AddTracks_SendsEachRepeatedTrackOnce(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	plRepo := &addTracksRecorder{InMemoryPlaylistRepo: library.NewInMemoryPlaylistRepo()}
	trRepo := library.NewInMemoryTrackRepo()
	pl := librarytest.SeedPlaylist(t, plRepo.InMemoryPlaylistRepo, userId, "My Playlist")
	a := librarytest.SeedTrack(t, trRepo, userId, "A", "Artist", "Album")
	b := librarytest.SeedTrack(t, trRepo, userId, "B", "Artist", "Album")
	svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

	added, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{a.ID, b.ID, a.ID, b.ID})
	if err != nil {
		t.Fatalf("AddTracks: %v", err)
	}
	if added != 2 {
		t.Fatalf("added = %d, want 2", added)
	}
	want := [][]library.TrackId{{a.ID, b.ID}}
	if !reflect.DeepEqual(plRepo.asked, want) {
		t.Fatalf("repository asked to add %v, want %v: each track once, in request order", plRepo.asked, want)
	}
}

func TestPlaylistMembershipService_RemoveTrack(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*library.InMemoryPlaylistRepo) (library.PlaylistId, library.TrackId)
		wantErr error
	}{
		{
			name: "track removed from playlist",
			setup: func(plRepo *library.InMemoryPlaylistRepo) (library.PlaylistId, library.TrackId) {
				pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
				trackId := librarytest.NewTrackId()
				return pl.ID, trackId
			},
			wantErr: nil,
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *library.InMemoryPlaylistRepo) (library.PlaylistId, library.TrackId) {
				return librarytest.NewPlaylistId(), librarytest.NewTrackId()
			},
			wantErr: library.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *library.InMemoryPlaylistRepo) (library.PlaylistId, library.TrackId) {
				plRepo.ErrOnRemoveTrack = errRepo
				return librarytest.NewPlaylistId(), librarytest.NewTrackId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := library.NewInMemoryPlaylistRepo()
			trRepo := library.NewInMemoryTrackRepo()
			playlistId, trackId := tt.setup(plRepo)
			svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

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
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	seedPlaylistWithTracks := func(t *testing.T, plRepo *library.InMemoryPlaylistRepo, trackIds ...library.TrackId) *library.Playlist {
		t.Helper()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		for _, id := range trackIds {
			if err := pl.AddTrack(id, time.Now()); err != nil {
				t.Fatalf("seed AddTrack: %v", err)
			}
		}
		return pl
	}

	t.Run("removes every listed track in one repository call", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		first, second, third := librarytest.NewTrackId(), librarytest.NewTrackId(), librarytest.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, first, second, third)
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []library.TrackId{first, third})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 2 {
			t.Fatalf("removed = %d, want 2", removed)
		}
		if !reflect.DeepEqual(plRepo.Removed, []library.TrackId{first, third}) {
			t.Fatalf("persisted = %v, want %v", plRepo.Removed, []library.TrackId{first, third})
		}
	})

	t.Run("leaves the surviving tracks at contiguous positions", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		first, second, third := librarytest.NewTrackId(), librarytest.NewTrackId(), librarytest.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, first, second, third)
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []library.TrackId{first}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []library.PlaylistTrack{
			{TrackId: second, Position: 0},
			{TrackId: third, Position: 1},
		}
		if !reflect.DeepEqual(pl.Tracks, want) {
			t.Fatalf("tracks = %v, want %v", pl.Tracks, want)
		}
	})

	t.Run("skips a track that is not in the playlist rather than failing the batch", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		member := librarytest.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, member)
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []library.TrackId{member, librarytest.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 1 {
			t.Fatalf("removed = %d, want 1", removed)
		}
	})

	t.Run("removes nothing and reports zero when nothing was a member", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		member := librarytest.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, member)
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []library.TrackId{librarytest.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 0 || len(plRepo.Removed) != 0 {
			t.Fatalf("removed = %d (persisted %v), want 0", removed, plRepo.Removed)
		}
		if want := []library.PlaylistTrack{{TrackId: member, Position: 0}}; !reflect.DeepEqual(pl.Tracks, want) {
			t.Fatalf("tracks = %v, want %v", pl.Tracks, want)
		}
	})

	t.Run("never loads the playlist's track list", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		first, second := librarytest.NewTrackId(), librarytest.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, first, second)
		plRepo.ErrOnGetWithTracks = errRepo
		plRepo.ErrOnGetTrackOrder = errRepo
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []library.TrackId{first}); err != nil {
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
		plRepo := library.NewInMemoryPlaylistRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
		svc := library.MustNewPlaylistMembershipService(plRepo, library.NewInMemoryTrackRepo(), events.NoopPublisher(), time.Now)

		assertBatchCap(t, func(ids []library.TrackId) error {
			_, err := svc.RemoveTracks(ctx, userId, pl.ID, ids)
			return err
		})
	})

	t.Run("playlist not found returns ErrPlaylistNotFound", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		_, err := svc.RemoveTracks(ctx, userId, librarytest.NewPlaylistId(), []library.TrackId{librarytest.NewTrackId()})

		if !errors.Is(err, library.ErrPlaylistNotFound) {
			t.Fatalf("error = %v, want %v", err, library.ErrPlaylistNotFound)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		member := librarytest.NewTrackId()
		pl := seedPlaylistWithTracks(t, plRepo, member)
		plRepo.ErrOnRemoveTracks = errRepo
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

		_, err := svc.RemoveTracks(ctx, userId, pl.ID, []library.TrackId{member})

		if !errors.Is(err, errRepo) {
			t.Fatalf("error = %v, want %v", err, errRepo)
		}
	})
}

func TestPlaylistMembershipService_Reorder(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*library.InMemoryPlaylistRepo) (library.PlaylistId, []library.TrackId)
		wantErr error
	}{
		{
			name: "valid reorder succeeds",
			setup: func(plRepo *library.InMemoryPlaylistRepo) (library.PlaylistId, []library.TrackId) {
				pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
				t1 := librarytest.NewTrackId()
				t2 := librarytest.NewTrackId()
				pl.Tracks = []library.PlaylistTrack{
					{TrackId: t1, Position: 0},
					{TrackId: t2, Position: 1},
				}
				plRepo.Seed(pl)
				return pl.ID, []library.TrackId{t2, t1}
			},
			wantErr: nil,
		},
		{
			name: "playlist not found returns ErrPlaylistNotFound",
			setup: func(plRepo *library.InMemoryPlaylistRepo) (library.PlaylistId, []library.TrackId) {
				return librarytest.NewPlaylistId(), []library.TrackId{}
			},
			wantErr: library.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(plRepo *library.InMemoryPlaylistRepo) (library.PlaylistId, []library.TrackId) {
				plRepo.ErrOnGetTrackOrder = errRepo
				return librarytest.NewPlaylistId(), []library.TrackId{}
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := library.NewInMemoryPlaylistRepo()
			trRepo := library.NewInMemoryTrackRepo()
			playlistId, trackIds := tt.setup(plRepo)
			svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, events.NoopPublisher(), time.Now)

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
	userId := librarytest.UserId()
	first, second := librarytest.NewTrackId(), librarytest.NewTrackId()

	cases := []struct {
		name  string
		order []library.TrackId
	}{
		{"a member is missing", []library.TrackId{second}},
		{"an extra track is listed", []library.TrackId{second, first, librarytest.NewTrackId()}},
		{"a stranger replaces a member", []library.TrackId{second, librarytest.NewTrackId()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plRepo := library.NewInMemoryPlaylistRepo()
			pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
			for _, id := range []library.TrackId{first, second} {
				if err := pl.AddTrack(id, time.Now()); err != nil {
					t.Fatalf("seed AddTrack: %v", err)
				}
			}
			svc := library.MustNewPlaylistMembershipService(plRepo, library.NewInMemoryTrackRepo(), events.NoopPublisher(), time.Now)

			err := svc.Reorder(ctx, userId, pl.ID, tc.order)

			if httpStatus(err) != 400 {
				t.Fatalf("error = %v, want a 400 validation error", err)
			}
			want := []library.PlaylistTrack{{TrackId: first, Position: 0}, {TrackId: second, Position: 1}}
			if !reflect.DeepEqual(pl.Tracks, want) {
				t.Fatalf("tracks = %v, want the order left as %v", pl.Tracks, want)
			}
		})
	}
}

func TestPlaylistMembershipService_Reorder_AnUnknownPlaylistIsNotFoundWhateverTheOrder(t *testing.T) {
	svc := library.MustNewPlaylistMembershipService(library.NewInMemoryPlaylistRepo(), library.NewInMemoryTrackRepo(), events.NoopPublisher(), time.Now)

	err := svc.Reorder(context.Background(), librarytest.UserId(), librarytest.NewPlaylistId(), []library.TrackId{librarytest.NewTrackId()})

	if !errors.Is(err, library.ErrPlaylistNotFound) {
		t.Fatalf("error = %v, want ErrPlaylistNotFound rather than a complaint about the order", err)
	}
}

type playlistEditedAfterRead struct {
	*library.InMemoryPlaylistRepo
	added library.TrackId
}

func (r *playlistEditedAfterRead) GetTrackOrder(ctx context.Context, playlistId library.PlaylistId, userId sharedids.UserId) ([]library.TrackId, bool, error) {
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
	userId := librarytest.UserId()
	plRepo := library.NewInMemoryPlaylistRepo()
	pl := librarytest.SeedPlaylist(t, plRepo, userId, "My Playlist")
	first, second, third := librarytest.NewTrackId(), librarytest.NewTrackId(), librarytest.NewTrackId()
	pl.Tracks = []library.PlaylistTrack{
		{TrackId: first, Position: 0},
		{TrackId: second, Position: 1},
		{TrackId: third, Position: 2},
	}
	plRepo.Seed(pl)
	racing := &playlistEditedAfterRead{InMemoryPlaylistRepo: plRepo, added: librarytest.NewTrackId()}
	svc := library.MustNewPlaylistMembershipService(racing, library.NewInMemoryTrackRepo(), events.NoopPublisher(), time.Now)

	err := svc.Reorder(ctx, userId, pl.ID, []library.TrackId{third, second, first})

	if !errors.Is(err, library.ErrPlaylistChangedDuringReorder) {
		t.Fatalf("error = %v, want %v", err, library.ErrPlaylistChangedDuringReorder)
	}
	if httpStatus(err) != 400 {
		t.Fatalf("error = %v, want a 400 the client can retry from", err)
	}
}

type ownerBlindReadRepo struct {
	*library.InMemoryPlaylistRepo
}

func (r ownerBlindReadRepo) Exists(_ context.Context, id library.PlaylistId, _ sharedids.UserId) (bool, error) {
	_, ok := r.Playlists[id.String()]
	return ok, nil
}

func (r ownerBlindReadRepo) GetTrackOrder(_ context.Context, id library.PlaylistId, _ sharedids.UserId) ([]library.TrackId, bool, error) {
	p, ok := r.Playlists[id.String()]
	if !ok {
		return nil, false, nil
	}
	ids := make([]library.TrackId, len(p.Tracks))
	for i, t := range p.Tracks {
		ids[i] = t.TrackId
	}
	return ids, true, nil
}

func wireStrings(v any) ([]string, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

type recordingPublisher struct{ types []string }

func (p *recordingPublisher) Publish(_ context.Context, _ sharedids.UserId, payload events.Payload) {
	p.types = append(p.types, string(payload.EventType()))
}

func TestPlaylistMembershipService_ForeignWriteRefusedBelowLoadCheck(t *testing.T) {
	ctx := context.Background()
	victim := librarytest.UserId()
	attacker := sharedids.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))

	cases := []struct {
		name string
		call func(*library.PlaylistMembershipService, library.PlaylistId, []library.TrackId, library.TrackId) error
	}{
		{"AddTrack", func(s *library.PlaylistMembershipService, pl library.PlaylistId, _ []library.TrackId, own library.TrackId) error {
			return s.AddTrack(ctx, attacker, pl, own)
		}},
		{"AddTracks", func(s *library.PlaylistMembershipService, pl library.PlaylistId, _ []library.TrackId, own library.TrackId) error {
			_, err := s.AddTracks(ctx, attacker, pl, []library.TrackId{own})
			return err
		}},
		{"RemoveTrack", func(s *library.PlaylistMembershipService, pl library.PlaylistId, members []library.TrackId, _ library.TrackId) error {
			return s.RemoveTrack(ctx, attacker, pl, members[0])
		}},
		{"RemoveTracks", func(s *library.PlaylistMembershipService, pl library.PlaylistId, members []library.TrackId, _ library.TrackId) error {
			_, err := s.RemoveTracks(ctx, attacker, pl, members)
			return err
		}},
		{"Reorder", func(s *library.PlaylistMembershipService, pl library.PlaylistId, members []library.TrackId, _ library.TrackId) error {
			return s.Reorder(ctx, attacker, pl, []library.TrackId{members[1], members[0]})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plRepo := library.NewInMemoryPlaylistRepo()
			trRepo := library.NewInMemoryTrackRepo()
			pl := librarytest.SeedPlaylist(t, plRepo, victim, "Victim's Playlist")
			members := []library.TrackId{librarytest.NewTrackId(), librarytest.NewTrackId()}
			for _, id := range members {
				if err := pl.AddTrack(id, time.Now()); err != nil {
					t.Fatalf("seed AddTrack: %v", err)
				}
			}
			own := librarytest.SeedTrack(t, trRepo, attacker, "Attacker Track", "Artist", "Album")
			pub := &recordingPublisher{}
			svc := library.MustNewPlaylistMembershipService(ownerBlindReadRepo{plRepo}, trRepo, pub, time.Now)

			err := tc.call(svc, pl.ID, members, own.ID)

			if !errors.Is(err, library.ErrPlaylistNotFound) {
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
	logs := library.CaptureAuditLogs(t)
	userId := librarytest.UserId()
	plRepo := library.NewInMemoryPlaylistRepo()
	pl := librarytest.SeedPlaylist(t, plRepo, userId, "Mix")
	trackId := librarytest.NewTrackId()
	if err := pl.AddTrack(trackId, time.Now()); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	svc := library.MustNewPlaylistMembershipService(plRepo, library.NewInMemoryTrackRepo(), events.NoopPublisher(), time.Now)
	if err := svc.RemoveTrack(librarytest.ActingAs(userId), userId, pl.ID, trackId); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	library.AssertAttrs(t, logs.Find(t, "track removed from playlist"), map[string]string{
		"user_id":     userId.String(),
		"playlist_id": pl.ID.String(),
		"track_id":    trackId.String(),
	})
}

func TestPlaylistMembershipService_RemoveTracks_LogsActorAndObject(t *testing.T) {
	logs := library.CaptureAuditLogs(t)
	userId := librarytest.UserId()
	plRepo := library.NewInMemoryPlaylistRepo()
	pl := librarytest.SeedPlaylist(t, plRepo, userId, "Mix")
	a, b := librarytest.NewTrackId(), librarytest.NewTrackId()
	for _, id := range []library.TrackId{a, b} {
		if err := pl.AddTrack(id, time.Now()); err != nil {
			t.Fatalf("seed track: %v", err)
		}
	}
	absent := librarytest.NewTrackId()

	svc := library.MustNewPlaylistMembershipService(plRepo, library.NewInMemoryTrackRepo(), events.NoopPublisher(), time.Now)
	n, err := svc.RemoveTracks(librarytest.ActingAs(userId), userId, pl.ID, []library.TrackId{a, absent, b})
	if err != nil || n != 2 {
		t.Fatalf("RemoveTracks = %d, %v; want 2, nil", n, err)
	}

	rec := logs.Find(t, "tracks removed from playlist")
	library.AssertAttrs(t, rec, map[string]string{
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
	userId := sharedids.NewUserId(uuid.New())

	t.Run("remove track", func(t *testing.T) {
		pub := &library.InMemoryRecordingPublisher{}
		plRepo := library.NewInMemoryPlaylistRepo()
		track, _ := library.NewTrack(userId, "T", "A", "", time.Now, ids.Real)
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "PL")
		_ = pl.AddTrack(track.ID, time.Now())
		plRepo.SeedWithTracks(t, pl, []*library.Track{track})
		svc := library.MustNewPlaylistMembershipService(plRepo, library.NewInMemoryTrackRepo(), pub, time.Now)

		if err := svc.RemoveTrack(ctx, userId, pl.ID, track.ID); err != nil {
			t.Fatalf("remove track: %v", err)
		}
		p := pub.LastFields("track_removed_from_playlist")
		if p == nil || p["playlist_id"] != pl.ID.String() || p["track_id"] != track.ID.String() {
			t.Fatalf("track_removed_from_playlist payload = %v", p)
		}
	})

	t.Run("reorder", func(t *testing.T) {
		pub := &library.InMemoryRecordingPublisher{}
		plRepo := library.NewInMemoryPlaylistRepo()
		t1, _ := library.NewTrack(userId, "T1", "A", "", time.Now, ids.Real)
		t2, _ := library.NewTrack(userId, "T2", "A", "", time.Now, ids.Real)
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "PL")
		_ = pl.AddTrack(t1.ID, time.Now())
		_ = pl.AddTrack(t2.ID, time.Now())
		plRepo.SeedWithTracks(t, pl, []*library.Track{t1, t2})
		svc := library.MustNewPlaylistMembershipService(plRepo, library.NewInMemoryTrackRepo(), pub, time.Now)

		if err := svc.Reorder(ctx, userId, pl.ID, []library.TrackId{t2.ID, t1.ID}); err != nil {
			t.Fatalf("reorder: %v", err)
		}
		p := pub.LastFields("playlist_reordered")
		if p == nil || p["playlist_id"] != pl.ID.String() {
			t.Fatalf("playlist_reordered payload = %v", p)
		}
		ids, ok := wireStrings(p["track_ids"])
		if !ok || len(ids) != 2 || ids[0] != t2.ID.String() || ids[1] != t1.ID.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], t2.ID.String(), t1.ID.String())
		}
	})

	t.Run("add tracks names only the inserted tracks in request order", func(t *testing.T) {
		pub := &library.InMemoryRecordingPublisher{}
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		member := librarytest.SeedTrack(t, trRepo, userId, "Member", "A", "")
		b := librarytest.SeedTrack(t, trRepo, userId, "B", "A", "")
		a := librarytest.SeedTrack(t, trRepo, userId, "A", "A", "")
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "PL")
		_ = pl.AddTrack(member.ID, time.Now())
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, pub, time.Now)

		if _, err := svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{b.ID, member.ID, a.ID, b.ID}); err != nil {
			t.Fatalf("add tracks: %v", err)
		}
		p := pub.LastFields("tracks_added_to_playlist")
		ids, ok := wireStrings(p["track_ids"])
		if !ok || len(ids) != 2 || ids[0] != b.ID.String() || ids[1] != a.ID.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], b.ID.String(), a.ID.String())
		}
	})

	t.Run("remove tracks names only the removed tracks in request order", func(t *testing.T) {
		pub := &library.InMemoryRecordingPublisher{}
		plRepo := library.NewInMemoryPlaylistRepo()
		first, second, third := librarytest.NewTrackId(), librarytest.NewTrackId(), librarytest.NewTrackId()
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "PL")
		for _, id := range []library.TrackId{first, second, third} {
			_ = pl.AddTrack(id, time.Now())
		}
		svc := library.MustNewPlaylistMembershipService(plRepo, library.NewInMemoryTrackRepo(), pub, time.Now)

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []library.TrackId{third, librarytest.NewTrackId(), first, third}); err != nil {
			t.Fatalf("remove tracks: %v", err)
		}
		p := pub.LastFields("tracks_removed_from_playlist")
		ids, ok := wireStrings(p["track_ids"])
		if !ok || len(ids) != 2 || ids[0] != third.String() || ids[1] != first.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], third.String(), first.String())
		}
	})

	t.Run("no-op membership writes publish nothing", func(t *testing.T) {
		pub := &library.InMemoryRecordingPublisher{}
		plRepo := library.NewInMemoryPlaylistRepo()
		trRepo := library.NewInMemoryTrackRepo()
		member := librarytest.SeedTrack(t, trRepo, userId, "Member", "A", "")
		pl := librarytest.SeedPlaylist(t, plRepo, userId, "PL")
		_ = pl.AddTrack(member.ID, time.Now())
		svc := library.MustNewPlaylistMembershipService(plRepo, trRepo, pub, time.Now)

		_ = svc.AddTrack(ctx, userId, pl.ID, member.ID)
		_, _ = svc.AddTracks(ctx, userId, pl.ID, []library.TrackId{member.ID})
		_ = svc.RemoveTrack(ctx, userId, pl.ID, librarytest.NewTrackId())
		_, _ = svc.RemoveTracks(ctx, userId, pl.ID, []library.TrackId{librarytest.NewTrackId()})

		if len(pub.Events) != 0 {
			t.Fatalf("no-op writes published %v", pub.Events)
		}
	})
}
