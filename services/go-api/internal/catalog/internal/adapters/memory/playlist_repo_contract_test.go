package memory_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/internal/adapters/contract"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"testing"

	"github.com/google/uuid"
)

type playlistRepoWithTracks struct {
	*catalog.InMemoryPlaylistRepo
	tracks *catalog.InMemoryTrackRepo
}

func (r *playlistRepoWithTracks) SeedTrack(t *testing.T, userId shared.UserId) catalog.TrackId {
	t.Helper()
	track, err := catalog.NewTrack(userId, "Title-"+uuid.NewString()[:8], "Artist-"+uuid.NewString()[:8], "Album")
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
		tracks := catalog.NewInMemoryTrackRepo()
		repo := catalog.NewInMemoryPlaylistRepo()
		repo.Tracks = tracks
		return &playlistRepoWithTracks{InMemoryPlaylistRepo: repo, tracks: tracks}
	})
}
