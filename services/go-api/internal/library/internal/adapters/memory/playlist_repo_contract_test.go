package memory_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contract"
	"altune/go-api/internal/shared/ids"
	"context"
	"testing"

	"github.com/google/uuid"
)

type playlistRepoWithTracks struct {
	*library.InMemoryPlaylistRepo
	tracks *library.InMemoryTrackRepo
}

func (r *playlistRepoWithTracks) SeedTrack(t *testing.T, userId ids.UserId) library.TrackId {
	t.Helper()
	track, err := library.NewTrack(userId, "Title-"+uuid.NewString()[:8], "Artist-"+uuid.NewString()[:8], "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if _, _, err := r.tracks.Add(context.Background(), track); err != nil {
		t.Fatalf("Add track: %v", err)
	}
	return track.ID
}

func TestPlaylistRepoFake_Contract(t *testing.T) {
	contract.PlaylistRepositoryContract(t, func(*testing.T) contract.PlaylistRepositoryUnderTest {
		tracks := library.NewInMemoryTrackRepo()
		repo := library.NewInMemoryPlaylistRepo()
		repo.Tracks = tracks
		return &playlistRepoWithTracks{InMemoryPlaylistRepo: repo, tracks: tracks}
	})
}
