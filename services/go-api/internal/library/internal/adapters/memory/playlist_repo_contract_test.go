package memory_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contracttest"
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type playlistRepoWithTracks struct {
	*library.InMemoryPlaylistRepo
	tracks *library.InMemoryTrackRepo
}

func (r *playlistRepoWithTracks) SeedTrack(t *testing.T, userId ids.UserId) library.TrackId {
	t.Helper()
	track, err := library.NewTrack(userId, "Title-"+uuid.NewString()[:8], "Artist-"+uuid.NewString()[:8], "Album", time.Now)
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if _, _, err := r.tracks.Add(context.Background(), track); err != nil {
		t.Fatalf("Add track: %v", err)
	}
	return track.ID
}

func TestPlaylistRepoFake_Contract(t *testing.T) {
	contracttest.PlaylistRepositoryContract(t, func(*testing.T) contracttest.PlaylistRepositoryUnderTest {
		tracks := library.NewInMemoryTrackRepo()
		repo := library.NewInMemoryPlaylistRepo()
		repo.Tracks = tracks
		return &playlistRepoWithTracks{InMemoryPlaylistRepo: repo, tracks: tracks}
	})
}

func fullPlaylistRepo(t *testing.T, userId ids.UserId, size int) (*library.InMemoryPlaylistRepo, *library.Playlist) {
	t.Helper()
	repo := library.NewInMemoryPlaylistRepo()
	pl, err := library.NewPlaylist(userId, "Full", time.Now())
	if err != nil {
		t.Fatalf("NewPlaylist: %v", err)
	}
	for range size {
		if err := pl.AddTrack(library.NewTrackId(), pl.CreatedAt); err != nil {
			t.Fatalf("fill: %v", err)
		}
	}
	if err := repo.Create(context.Background(), pl); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return repo, pl
}

type stoppingTB struct {
	memory.TB
	failure string
}

func (s *stoppingTB) Helper() {}

func (s *stoppingTB) Fatalf(format string, args ...any) {
	s.failure = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func TestPlaylistRepoFake_SeedWithTracksFailsWhenATrackDoesNotFit(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	repo, pl := fullPlaylistRepo(t, userId, library.MaxPlaylistTracks)
	tb := &stoppingTB{}
	done := make(chan struct{})

	go func() {
		defer close(done)
		repo.SeedWithTracks(tb, pl, []*library.Track{{ID: library.NewTrackId()}})
	}()
	<-done

	if !strings.Contains(tb.failure, "playlist is full") {
		t.Fatalf("seed failure = %q, want it to report ErrPlaylistFull", tb.failure)
	}
}

func TestPlaylistRepoFake_AddTrackRefusesPastTheCap(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	repo, pl := fullPlaylistRepo(t, userId, library.MaxPlaylistTracks)

	err := repo.AddTrack(context.Background(), userId, pl.ID, library.NewTrackId())

	if !errors.Is(err, library.ErrPlaylistFull) {
		t.Fatalf("AddTrack past the cap: err = %v, want ErrPlaylistFull", err)
	}
}

func TestPlaylistRepoFake_AddTracksRefusesTheWholeBatchPastTheCap(t *testing.T) {
	userId := ids.NewUserId(uuid.New())
	repo, pl := fullPlaylistRepo(t, userId, library.MaxPlaylistTracks-1)
	batch := []library.TrackId{library.NewTrackId(), library.NewTrackId()}

	added, err := repo.AddTracks(context.Background(), userId, pl.ID, batch)

	if !errors.Is(err, library.ErrPlaylistFull) {
		t.Fatalf("AddTracks past the cap: err = %v, want ErrPlaylistFull", err)
	}
	if len(added) != 0 || len(pl.Tracks) != library.MaxPlaylistTracks-1 {
		t.Errorf("added %v, playlist holds %d, want nothing added and %d held", added, len(pl.Tracks), library.MaxPlaylistTracks-1)
	}
	if len(repo.Added) != 0 {
		t.Errorf("repo.Added = %v, want it rolled back", repo.Added)
	}
}
