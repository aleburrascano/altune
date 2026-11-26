package playlist_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const playlistCap = 1_000

func TestPlaylistLifecycleService_Create(t *testing.T) {
	ctx := context.Background()
	userId := library.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		plName  string
		setup   func(*library.InMemoryPlaylistRepo)
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
			setup: func(repo *library.InMemoryPlaylistRepo) {
				repo.ErrOnCreate = errRepo
			},
			wantErr: "db error",
		},
		{
			name:   "count error refuses the create",
			plName: "Good Name",
			setup: func(repo *library.InMemoryPlaylistRepo) {
				repo.ErrOnCount = errRepo
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := library.NewInMemoryPlaylistRepo()
			if tt.setup != nil {
				tt.setup(plRepo)
			}
			svc := library.NewPlaylistLifecycleService(plRepo)

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

type heldPlaylistsRepo struct {
	*library.InMemoryPlaylistRepo
	owner       ids.UserId
	held        int
	askedAtMost []int
}

func newHeldPlaylistsRepo(owner ids.UserId, held int) *heldPlaylistsRepo {
	return &heldPlaylistsRepo{InMemoryPlaylistRepo: library.NewInMemoryPlaylistRepo(), owner: owner, held: held}
}

func (r *heldPlaylistsRepo) CountForUser(ctx context.Context, userId ids.UserId, atMost int) (int, error) {
	r.askedAtMost = append(r.askedAtMost, atMost)
	if userId == r.owner {
		return min(r.held, atMost), nil
	}
	return r.InMemoryPlaylistRepo.CountForUser(ctx, userId, atMost)
}

func (r *heldPlaylistsRepo) assertAskedForTheCap(t *testing.T) {
	t.Helper()
	if len(r.askedAtMost) != 1 || r.askedAtMost[0] != playlistCap {
		t.Errorf("CountForUser bounds = %v, want one count bounded at the %d-playlist cap", r.askedAtMost, playlistCap)
	}
}

func TestPlaylistLifecycleService_Create_RejectsPastUserCap(t *testing.T) {
	ctx := context.Background()
	userId := library.FixtureUserId()
	plRepo := newHeldPlaylistsRepo(userId, playlistCap)
	svc := library.NewPlaylistLifecycleService(plRepo)

	playlist, err := svc.Create(ctx, userId, "One Too Many")

	if !errors.Is(err, library.ErrTooManyPlaylists) {
		t.Fatalf("error = %v, want ErrTooManyPlaylists", err)
	}
	if playlist != nil {
		t.Fatalf("playlist = %+v, want nil", playlist)
	}
	if len(plRepo.Playlists) != 0 {
		t.Fatalf("stored playlists = %d, want 0: the refused create must not insert", len(plRepo.Playlists))
	}
	plRepo.assertAskedForTheCap(t)
}

func TestPlaylistLifecycleService_Create_AllowsTheLastPlaylistUnderTheCap(t *testing.T) {
	ctx := context.Background()
	userId := library.FixtureUserId()
	plRepo := newHeldPlaylistsRepo(userId, playlistCap-1)
	svc := library.NewPlaylistLifecycleService(plRepo)

	playlist, err := svc.Create(ctx, userId, "The Last One")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(plRepo.Playlists) != 1 || plRepo.Playlists[playlist.ID.String()] == nil {
		t.Fatalf("stored playlists = %v, want the one created", plRepo.Playlists)
	}
	plRepo.assertAskedForTheCap(t)
}

func TestPlaylistLifecycleService_Create_CapCountsOnlyTheCallersPlaylists(t *testing.T) {
	ctx := context.Background()
	plRepo := newHeldPlaylistsRepo(library.FixtureOtherUserId(), playlistCap)
	svc := library.NewPlaylistLifecycleService(plRepo)

	playlist, err := svc.Create(ctx, library.FixtureUserId(), "Mine")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if playlist == nil {
		t.Fatal("playlist = nil, want created: another owner's rows are not this caller's cap")
	}
	plRepo.assertAskedForTheCap(t)
}

func TestPlaylistLifecycleService_Create_StampsWallClockInUTC(t *testing.T) {
	svc := library.NewPlaylistLifecycleService(library.NewInMemoryPlaylistRepo())

	before := time.Now()
	pl, err := svc.Create(context.Background(), library.FixtureUserId(), "Clocked")
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
	userId := library.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name       string
		setup      func(*library.InMemoryPlaylistRepo) library.PlaylistId
		wantTracks int
		wantErr    error
	}{
		{
			name: "found playlist with tracks",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				pl := library.SeedPlaylist(t, repo, userId, "Rock")
				track, _ := library.NewTrack(userId, "Track", "Artist", "Album")
				repo.SeedWithTracks(t, pl, []*library.Track{track})
				return pl.ID
			},
			wantTracks: 1,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				return library.NewPlaylistId()
			},
			wantErr: library.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				repo.ErrOnGetWithTracks = errRepo
				return library.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := library.NewInMemoryPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := library.NewPlaylistLifecycleService(plRepo)

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
	userId := library.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*library.InMemoryPlaylistRepo) library.PlaylistId
		wantErr error
	}{
		{
			name: "existing playlist is deleted",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				pl := library.SeedPlaylist(t, repo, userId, "To Delete")
				return pl.ID
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				return library.NewPlaylistId()
			},
			wantErr: library.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				repo.ErrOnDelete = errRepo
				return library.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := library.NewInMemoryPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := library.NewPlaylistLifecycleService(plRepo)

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
	userId := library.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		newName string
		setup   func(*library.InMemoryPlaylistRepo) library.PlaylistId
		wantErr string
	}{
		{
			name:    "valid rename succeeds",
			newName: "New Name",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				pl := library.SeedPlaylist(t, repo, userId, "Old Name")
				return pl.ID
			},
		},
		{
			name:    "not found returns ErrPlaylistNotFound",
			newName: "New Name",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				return library.NewPlaylistId()
			},
			wantErr: library.ErrPlaylistNotFound.Error(),
		},
		{
			name:    "empty name returns validation error",
			newName: "",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				pl := library.SeedPlaylist(t, repo, userId, "Has Name")
				return pl.ID
			},
			wantErr: "playlist name required",
		},
		{
			name:    "repo error on GetByID propagates",
			newName: "New Name",
			setup: func(repo *library.InMemoryPlaylistRepo) library.PlaylistId {
				repo.ErrOnGetByID = errRepo
				return library.NewPlaylistId()
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := library.NewInMemoryPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := library.NewPlaylistLifecycleService(plRepo)

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

type playlistDeletedAfterRead struct {
	*library.InMemoryPlaylistRepo
}

func (r *playlistDeletedAfterRead) GetByID(ctx context.Context, id library.PlaylistId, userId ids.UserId) (*library.Playlist, library.PlaylistSummary, error) {
	playlist, summary, err := r.InMemoryPlaylistRepo.GetByID(ctx, id, userId)
	if playlist == nil || err != nil {
		return playlist, summary, err
	}
	if _, err := r.Delete(ctx, id, userId); err != nil {
		return nil, library.PlaylistSummary{}, err
	}
	return playlist, summary, nil
}

func TestPlaylistLifecycleService_Rename_RefusesAPlaylistDeletedAfterTheRead(t *testing.T) {
	ctx := context.Background()
	userId := library.FixtureUserId()
	plRepo := library.NewInMemoryPlaylistRepo()
	pl := library.SeedPlaylist(t, plRepo, userId, "Old Name")
	pub := &library.InMemoryRecordingPublisher{}
	svc := library.NewPlaylistLifecycleService(&playlistDeletedAfterRead{plRepo}, library.WithPlaylistLifecycleEvents(pub))

	_, _, err := svc.Rename(ctx, userId, pl.ID, "New Name")

	if !errors.Is(err, library.ErrPlaylistNotFound) {
		t.Fatalf("error = %v, want %v", err, library.ErrPlaylistNotFound)
	}
	if payload := pub.Last(events.TypePlaylistRenamed); payload != nil {
		t.Fatalf("published a rename of a deleted playlist: %v", payload)
	}
}

func TestPlaylistLifecycleService_Rename_PublishesTheNewName(t *testing.T) {
	ctx := context.Background()
	userId := library.FixtureUserId()
	plRepo := library.NewInMemoryPlaylistRepo()
	pl := library.SeedPlaylist(t, plRepo, userId, "Old")
	pub := &library.InMemoryRecordingPublisher{}
	svc := library.NewPlaylistLifecycleService(plRepo, library.WithPlaylistLifecycleEvents(pub))

	if _, _, err := svc.Rename(ctx, userId, pl.ID, "New Name"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	p := pub.Last("playlist_renamed")
	if p == nil || p["playlist_id"] != pl.ID.String() || p["name"] != "New Name" {
		t.Fatalf("playlist_renamed payload = %v", p)
	}
}
