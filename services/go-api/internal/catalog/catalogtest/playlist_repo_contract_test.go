package catalogtest_test

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"

	"github.com/google/uuid"
)

type playlistRepoWithTracks struct {
	*catalogtest.PlaylistRepo
	tracks *catalogtest.TrackRepo
}

func (r *playlistRepoWithTracks) SeedTrack(t *testing.T, userId shared.UserId) domain.TrackId {
	t.Helper()
	track, err := domain.NewTrack(userId, "Title-"+uuid.NewString()[:8], "Artist-"+uuid.NewString()[:8], "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if _, _, err := r.tracks.Add(context.Background(), track); err != nil {
		t.Fatalf("Add track: %v", err)
	}
	return track.ID
}

func TestPlaylistRepoFake_Contract(t *testing.T) {
	catalogtest.PlaylistRepositoryContract(t, func(*testing.T) catalogtest.PlaylistRepositoryUnderTest {
		tracks := catalogtest.NewTrackRepo()
		repo := catalogtest.NewPlaylistRepo()
		repo.Tracks = tracks
		return &playlistRepoWithTracks{PlaylistRepo: repo, tracks: tracks}
	})
}
