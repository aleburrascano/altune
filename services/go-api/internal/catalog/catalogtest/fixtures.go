package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"testing"
	"time"

	"github.com/google/uuid"
)

func UserId() shared.UserId {
	return shared.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
}

func OtherUserId() shared.UserId {
	return shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
}

func SeedTrack(t *testing.T, repo *TrackRepo, userId shared.UserId, title, artist, album string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, title, artist, album)
	if err != nil {
		t.Fatalf("seedTrack: %v", err)
	}
	repo.Seed(track)
	return track
}

func SeedReadyTrack(t *testing.T, repo *TrackRepo, userId shared.UserId, title, artist, album, audioRef string) *domain.Track {
	t.Helper()
	track := SeedTrack(t, repo, userId, title, artist, album)
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("seedReadyTrack: %v", err)
	}
	return track
}

func SeedPlaylist(t *testing.T, repo *PlaylistRepo, userId shared.UserId, name string) *domain.Playlist {
	t.Helper()
	playlist, err := domain.NewPlaylist(userId, name, time.Now())
	if err != nil {
		t.Fatalf("seedPlaylist: %v", err)
	}
	repo.Seed(playlist)
	return playlist
}
