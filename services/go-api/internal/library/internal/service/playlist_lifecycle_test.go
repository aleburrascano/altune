package service_test

import (
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const playlistCap = 1_000

func TestNewPlaylistLifecycleService_RefusesAMissingCollaborator(t *testing.T) {
	repo := memory.NewPlaylistRepo()
	pub := events.NoopPublisher()
	tests := []struct {
		name string
		make func() (*service.PlaylistLifecycleService, error)
		want error
	}{
		{"repository", func() (*service.PlaylistLifecycleService, error) {
			return service.NewPlaylistLifecycleService(nil, pub, time.Now, ids.Real)
		}, domain.ErrPlaylistCollaboratorRequired},
		{"events", func() (*service.PlaylistLifecycleService, error) {
			return service.NewPlaylistLifecycleService(repo, nil, time.Now, ids.Real)
		}, domain.ErrPlaylistCollaboratorRequired},
		{"clock", func() (*service.PlaylistLifecycleService, error) {
			return service.NewPlaylistLifecycleService(repo, pub, nil, ids.Real)
		}, domain.ErrPlaylistClockRequired},
		{"id source", func() (*service.PlaylistLifecycleService, error) {
			return service.NewPlaylistLifecycleService(repo, pub, time.Now, nil)
		}, domain.ErrPlaylistIDSourceRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := tt.make()

			if !errors.Is(err, tt.want) || svc != nil {
				t.Fatalf("construction without %s = (%v, %v), want %v and no service", tt.name, svc, err, tt.want)
			}
			if !strings.Contains(err.Error(), "NewPlaylistLifecycleService") {
				t.Fatalf("error %q does not name the constructor", err)
			}
		})
	}
}

func TestMustNewPlaylistLifecycleService_PanicsOnANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustNewPlaylistLifecycleService accepted a nil clock")
		}
	}()

	service.MustNewPlaylistLifecycleService(memory.NewPlaylistRepo(), events.NoopPublisher(), nil, ids.Real)
}

func TestPlaylistLifecycleService_Create(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		plName  string
		setup   func(*memory.PlaylistRepo)
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
			setup: func(repo *memory.PlaylistRepo) {
				repo.ErrOnCreate = errRepo
			},
			wantErr: "db error",
		},
		{
			name:   "count error refuses the create",
			plName: "Good Name",
			setup: func(repo *memory.PlaylistRepo) {
				repo.ErrOnCount = errRepo
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			if tt.setup != nil {
				tt.setup(plRepo)
			}
			svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)

			playlist, _, err := svc.Create(ctx, userId, tt.plName, nil)

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
	*memory.PlaylistRepo
	owner       ids.UserId
	held        int
	askedAtMost []int
}

func newHeldPlaylistsRepo(owner ids.UserId, held int) *heldPlaylistsRepo {
	return &heldPlaylistsRepo{PlaylistRepo: memory.NewPlaylistRepo(), owner: owner, held: held}
}

func (r *heldPlaylistsRepo) CountForUser(ctx context.Context, userId ids.UserId, atMost int) (int, error) {
	r.askedAtMost = append(r.askedAtMost, atMost)
	if userId == r.owner {
		return min(r.held, atMost), nil
	}
	return r.PlaylistRepo.CountForUser(ctx, userId, atMost)
}

func (r *heldPlaylistsRepo) assertAskedForTheCap(t *testing.T) {
	t.Helper()
	if len(r.askedAtMost) != 1 || r.askedAtMost[0] != playlistCap {
		t.Errorf("CountForUser bounds = %v, want one count bounded at the %d-playlist cap", r.askedAtMost, playlistCap)
	}
}

func TestPlaylistLifecycleService_Create_RejectsPastUserCap(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	plRepo := newHeldPlaylistsRepo(userId, playlistCap)
	svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)

	playlist, _, err := svc.Create(ctx, userId, "One Too Many", nil)

	if !errors.Is(err, domain.ErrTooManyPlaylists) {
		t.Fatalf("error = %v, want domain.ErrTooManyPlaylists", err)
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
	userId := librarytest.UserId()
	plRepo := newHeldPlaylistsRepo(userId, playlistCap-1)
	svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)

	playlist, _, err := svc.Create(ctx, userId, "The Last One", nil)
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
	plRepo := newHeldPlaylistsRepo(librarytest.OtherUserId(), playlistCap)
	svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)

	playlist, _, err := svc.Create(ctx, librarytest.UserId(), "Mine", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if playlist == nil {
		t.Fatal("playlist = nil, want created: another owner's rows are not this caller's cap")
	}
	plRepo.assertAskedForTheCap(t)
}

func TestPlaylistLifecycleService_Create_ARepeatedKeyReturnsTheFirstPlaylist(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	plRepo := newHeldPlaylistsRepo(userId, 0)
	svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)
	key := "retry-1"

	first, created, err := svc.Create(ctx, userId, "Road Trip", &key)
	if err != nil || !created {
		t.Fatalf("first Create = (%v, %v), want created", created, err)
	}
	again, createdAgain, err := svc.Create(ctx, userId, "Road Trip", &key)
	if err != nil {
		t.Fatalf("repeat Create: %v", err)
	}
	if createdAgain {
		t.Error("the repeat reported created, want the existing playlist")
	}
	if again.ID != first.ID {
		t.Errorf("repeat id = %v, want %v", again.ID, first.ID)
	}
	if len(plRepo.Playlists) != 1 {
		t.Errorf("stored playlists = %d, want 1", len(plRepo.Playlists))
	}
}

func TestPlaylistLifecycleService_Create_ARepeatAtTheCapStillReturnsTheExisting(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	plRepo := newHeldPlaylistsRepo(userId, playlistCap-1)
	svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)
	key := "last-slot"
	first, _, err := svc.Create(ctx, userId, "Last", &key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	again, created, err := svc.Create(ctx, userId, "Last", &key)

	if err != nil || created || again.ID != first.ID {
		t.Fatalf("repeat at the cap = (%v, %v, %v), want the existing playlist", again, created, err)
	}
}

func TestPlaylistLifecycleService_Create_PublishesPlaylistCreatedOnlyWhenCreated(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	pub := &memory.RecordingPlaylistPublisher{}
	svc := service.MustNewPlaylistLifecycleService(memory.NewPlaylistRepo(), pub, time.Now, ids.Real)
	first, second := "a", "b"

	svc.Create(ctx, userId, "One", &first)
	svc.Create(ctx, userId, "One", &first)
	svc.Create(ctx, userId, "Two", &second)

	if got := len(pub.Events); got != 2 {
		t.Fatalf("published %d events, want 2: one per created playlist and none for the repeat", got)
	}
}

type missingLookupRepo struct {
	*memory.PlaylistRepo
}

func (missingLookupRepo) GetByIdempotencyKey(context.Context, ids.UserId, string) (*domain.Playlist, bool, error) {
	return nil, false, nil
}

func TestPlaylistLifecycleService_Create_ALostRaceReturnsTheWinnerWithoutAnEvent(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	repo := missingLookupRepo{memory.NewPlaylistRepo()}
	pub := &memory.RecordingPlaylistPublisher{}
	svc := service.MustNewPlaylistLifecycleService(repo, pub, time.Now, ids.Real)
	key := "raced"
	winner, _, err := svc.Create(ctx, userId, "Winner", &key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	published := len(pub.Events)

	got, created, err := svc.Create(ctx, userId, "Loser", &key)

	if err != nil || created || got == nil || got.ID != winner.ID {
		t.Fatalf("lost race = (%v, %v, %v), want the winner and created=false", got, created, err)
	}
	if len(pub.Events) != published {
		t.Error("the lost race published a PlaylistCreated event")
	}
}

func TestPlaylistLifecycleService_Create_KeysAreScopedPerUser(t *testing.T) {
	ctx := context.Background()
	plRepo := memory.NewPlaylistRepo()
	svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)
	key := "same"

	mine, _, _ := svc.Create(ctx, librarytest.UserId(), "Mine", &key)
	theirs, created, err := svc.Create(ctx, librarytest.OtherUserId(), "Theirs", &key)

	if err != nil || !created || theirs.ID == mine.ID {
		t.Fatalf("other user's create = (%v, %v, %v), want a separate playlist", theirs, created, err)
	}
}

func TestPlaylistLifecycleService_Create_RefusesAMalformedKey(t *testing.T) {
	svc := service.MustNewPlaylistLifecycleService(memory.NewPlaylistRepo(), events.NoopPublisher(), time.Now, ids.Real)
	key := strings.Repeat("k", 201)

	pl, _, err := svc.Create(context.Background(), librarytest.UserId(), "X", &key)

	if err == nil || pl != nil {
		t.Fatalf("Create with a 201-char key = (%v, %v), want a validation error", pl, err)
	}
}

func TestPlaylistLifecycleService_Create_StampsWallClockInUTC(t *testing.T) {
	svc := service.MustNewPlaylistLifecycleService(memory.NewPlaylistRepo(), events.NoopPublisher(), time.Now, ids.Real)

	before := time.Now()
	pl, _, err := svc.Create(context.Background(), librarytest.UserId(), "Clocked", nil)
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
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name       string
		setup      func(*memory.PlaylistRepo) domain.PlaylistId
		wantTracks int
		wantErr    error
	}{
		{
			name: "found playlist with tracks",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				pl := librarytest.SeedPlaylist(t, repo, userId, "Rock")
				track, _ := domain.NewTrack(domain.NewTrackInput{UserId: userId, Title: "Track", Artist: "Artist", Album: "Album"}, time.Now, ids.Real)
				repo.SeedWithTracks(t, pl, []*domain.Track{track})
				return pl.ID
			},
			wantTracks: 1,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				return librarytest.NewPlaylistId()
			},
			wantErr: domain.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				repo.ErrOnGetWithTracks = errRepo
				return librarytest.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)

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
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*memory.PlaylistRepo) domain.PlaylistId
		wantErr error
	}{
		{
			name: "existing playlist is deleted",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				pl := librarytest.SeedPlaylist(t, repo, userId, "To Delete")
				return pl.ID
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrPlaylistNotFound",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				return librarytest.NewPlaylistId()
			},
			wantErr: domain.ErrPlaylistNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				repo.ErrOnDelete = errRepo
				return librarytest.NewPlaylistId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)

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
	userId := librarytest.UserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		newName string
		setup   func(*memory.PlaylistRepo) domain.PlaylistId
		wantErr string
	}{
		{
			name:    "valid rename succeeds",
			newName: "New Name",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				pl := librarytest.SeedPlaylist(t, repo, userId, "Old Name")
				return pl.ID
			},
		},
		{
			name:    "not found returns ErrPlaylistNotFound",
			newName: "New Name",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				return librarytest.NewPlaylistId()
			},
			wantErr: domain.ErrPlaylistNotFound.Error(),
		},
		{
			name:    "empty name returns validation error",
			newName: "",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				pl := librarytest.SeedPlaylist(t, repo, userId, "Has Name")
				return pl.ID
			},
			wantErr: "playlist name required",
		},
		{
			name:    "repo error on GetByID propagates",
			newName: "New Name",
			setup: func(repo *memory.PlaylistRepo) domain.PlaylistId {
				repo.ErrOnGetByID = errRepo
				return librarytest.NewPlaylistId()
			},
			wantErr: "db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			playlistId := tt.setup(plRepo)
			svc := service.MustNewPlaylistLifecycleService(plRepo, events.NoopPublisher(), time.Now, ids.Real)

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
	*memory.PlaylistRepo
}

func (r *playlistDeletedAfterRead) GetByID(ctx context.Context, id domain.PlaylistId, userId ids.UserId) (*domain.Playlist, domain.PlaylistSummary, error) {
	playlist, summary, err := r.PlaylistRepo.GetByID(ctx, id, userId)
	if playlist == nil || err != nil {
		return playlist, summary, err
	}
	if _, err := r.Delete(ctx, id, userId); err != nil {
		return nil, domain.PlaylistSummary{}, err
	}
	return playlist, summary, nil
}

func TestPlaylistLifecycleService_Rename_RefusesAPlaylistDeletedAfterTheRead(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	plRepo := memory.NewPlaylistRepo()
	pl := librarytest.SeedPlaylist(t, plRepo, userId, "Old Name")
	pub := &memory.RecordingPlaylistPublisher{}
	svc := service.MustNewPlaylistLifecycleService(&playlistDeletedAfterRead{plRepo}, pub, time.Now, ids.Real)

	_, _, err := svc.Rename(ctx, userId, pl.ID, "New Name")

	if !errors.Is(err, domain.ErrPlaylistNotFound) {
		t.Fatalf("error = %v, want %v", err, domain.ErrPlaylistNotFound)
	}
	if payload := pub.Last(events.TypePlaylistRenamed); payload != nil {
		t.Fatalf("published a rename of a deleted playlist: %v", payload)
	}
}

func TestPlaylistLifecycleService_Rename_PublishesTheNewName(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	plRepo := memory.NewPlaylistRepo()
	pl := librarytest.SeedPlaylist(t, plRepo, userId, "Old")
	pub := &memory.RecordingPlaylistPublisher{}
	svc := service.MustNewPlaylistLifecycleService(plRepo, pub, time.Now, ids.Real)

	if _, _, err := svc.Rename(ctx, userId, pl.ID, "New Name"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	p := pub.LastFields("playlist_renamed")
	if p == nil || p["playlist_id"] != pl.ID.String() || p["name"] != "New Name" {
		t.Fatalf("playlist_renamed payload = %v", p)
	}
}
