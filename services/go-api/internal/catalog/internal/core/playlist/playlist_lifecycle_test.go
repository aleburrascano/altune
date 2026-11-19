package playlist_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/shared/events"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const playlistCap = 1_000

func TestPlaylistLifecycleService_Create(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		plName  string
		setup   func(*catalog.InMemoryPlaylistRepo)
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
			setup: func(repo *catalog.InMemoryPlaylistRepo) {
				repo.ErrOnCreate = errRepo
			},
			wantErr: "db error",
		},
		{
			name:   "count error refuses the create",
			plName: "Good Name",
			setup: func(repo *catalog.InMemoryPlaylistRepo) {
				repo.ErrOnCount = errRepo
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			if tt.setup != nil {
				tt.setup(plRepo)
			}
			svc := catalog.NewPlaylistLifecycleService(plRepo)

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
	*catalog.InMemoryPlaylistRepo
	owner       shared.UserId
	held        int
	askedAtMost []int
}

func newHeldPlaylistsRepo(owner shared.UserId, held int) *heldPlaylistsRepo {
	return &heldPlaylistsRepo{InMemoryPlaylistRepo: catalog.NewInMemoryPlaylistRepo(), owner: owner, held: held}
}

func (r *heldPlaylistsRepo) CountForUser(ctx context.Context, userId shared.UserId, atMost int) (int, error) {
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
	userId := catalog.FixtureUserId()
	plRepo := newHeldPlaylistsRepo(userId, playlistCap)
	svc := catalog.NewPlaylistLifecycleService(plRepo)

	playlist, err := svc.Create(ctx, userId, "One Too Many")

	if !errors.Is(err, catalog.ErrTooManyPlaylists) {
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
	userId := catalog.FixtureUserId()
	plRepo := newHeldPlaylistsRepo(userId, playlistCap-1)
	svc := catalog.NewPlaylistLifecycleService(plRepo)

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
	plRepo := newHeldPlaylistsRepo(catalog.FixtureOtherUserId(), playlistCap)
	svc := catalog.NewPlaylistLifecycleService(plRepo)

	playlist, err := svc.Create(ctx, catalog.FixtureUserId(), "Mine")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if playlist == nil {
		t.Fatal("playlist = nil, want created: another owner's rows are not this caller's cap")
	}
	plRepo.assertAskedForTheCap(t)
}

func TestPlaylistLifecycleService_Create_StampsWallClockInUTC(t *testing.T) {
	svc := catalog.NewPlaylistLifecycleService(catalog.NewInMemoryPlaylistRepo())

	before := time.Now()
	pl, err := svc.Create(context.Background(), catalog.FixtureUserId(), "Clocked")
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
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name       string
		setup      func(*catalog.InMemoryPlaylistRepo) catalog.PlaylistId
		wantTracks int
		wantErr    error
	}{
		{
			name: "found playlist with tracks",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				pl := catalog.SeedPlaylist(t, repo, userId, "Rock")
				track, _ := catalog.NewTrack(userId, "Track", "Artist", "Album")
				repo.SeedWithTracks(pl, []*catalog.Track{track})
				return pl.ID
			},
			wantTracks: 1,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				return catalog.NewPlaylistId()
			},
			wantErr: catalog.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				repo.ErrOnGetWithTracks = errRepo
				return catalog.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := catalog.NewPlaylistLifecycleService(plRepo)

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
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalog.InMemoryPlaylistRepo) catalog.PlaylistId
		wantErr error
	}{
		{
			name: "existing playlist is deleted",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				pl := catalog.SeedPlaylist(t, repo, userId, "To Delete")
				return pl.ID
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				return catalog.NewPlaylistId()
			},
			wantErr: catalog.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				repo.ErrOnDelete = errRepo
				return catalog.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := catalog.NewPlaylistLifecycleService(plRepo)

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
	userId := catalog.FixtureUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		newName string
		setup   func(*catalog.InMemoryPlaylistRepo) catalog.PlaylistId
		wantErr string
	}{
		{
			name:    "valid rename succeeds",
			newName: "New Name",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				pl := catalog.SeedPlaylist(t, repo, userId, "Old Name")
				return pl.ID
			},
		},
		{
			name:    "not found returns ErrPlaylistNotFound",
			newName: "New Name",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				return catalog.NewPlaylistId()
			},
			wantErr: catalog.ErrPlaylistNotFound.Error(),
		},
		{
			name:    "empty name returns validation error",
			newName: "",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				pl := catalog.SeedPlaylist(t, repo, userId, "Has Name")
				return pl.ID
			},
			wantErr: "playlist name required",
		},
		{
			name:    "repo error on GetByID propagates",
			newName: "New Name",
			setup: func(repo *catalog.InMemoryPlaylistRepo) catalog.PlaylistId {
				repo.ErrOnGetByID = errRepo
				return catalog.NewPlaylistId()
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalog.NewInMemoryPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := catalog.NewPlaylistLifecycleService(plRepo)

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
	*catalog.InMemoryPlaylistRepo
}

func (r *playlistDeletedAfterRead) GetByID(ctx context.Context, id catalog.PlaylistId, userId shared.UserId) (*catalog.Playlist, catalog.PlaylistSummary, error) {
	playlist, summary, err := r.InMemoryPlaylistRepo.GetByID(ctx, id, userId)
	if playlist == nil || err != nil {
		return playlist, summary, err
	}
	if _, err := r.Delete(ctx, id, userId); err != nil {
		return nil, catalog.PlaylistSummary{}, err
	}
	return playlist, summary, nil
}

func TestPlaylistLifecycleService_Rename_RefusesAPlaylistDeletedAfterTheRead(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	plRepo := catalog.NewInMemoryPlaylistRepo()
	pl := catalog.SeedPlaylist(t, plRepo, userId, "Old Name")
	pub := &catalog.InMemoryRecordingPublisher{}
	svc := catalog.NewPlaylistLifecycleService(&playlistDeletedAfterRead{plRepo}, catalog.WithPlaylistLifecycleEvents(pub))

	_, _, err := svc.Rename(ctx, userId, pl.ID, "New Name")

	if !errors.Is(err, catalog.ErrPlaylistNotFound) {
		t.Fatalf("error = %v, want %v", err, catalog.ErrPlaylistNotFound)
	}
	if payload := pub.Last(events.TypePlaylistRenamed); payload != nil {
		t.Fatalf("published a rename of a deleted playlist: %v", payload)
	}
}

func TestPlaylistLifecycleService_Rename_PublishesTheNewName(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	plRepo := catalog.NewInMemoryPlaylistRepo()
	pl := catalog.SeedPlaylist(t, plRepo, userId, "Old")
	pub := &catalog.InMemoryRecordingPublisher{}
	svc := catalog.NewPlaylistLifecycleService(plRepo, catalog.WithPlaylistLifecycleEvents(pub))

	if _, _, err := svc.Rename(ctx, userId, pl.ID, "New Name"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	p := pub.Last("playlist_renamed")
	if p == nil || p["playlist_id"] != pl.ID.String() || p["name"] != "New Name" {
		t.Fatalf("playlist_renamed payload = %v", p)
	}
}
