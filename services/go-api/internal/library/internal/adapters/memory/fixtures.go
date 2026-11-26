package memory

import (
	"altune/go-api/internal/library/domain"
	"altune/go-api/internal/shared/ids"
	"time"

	"github.com/google/uuid"
)

func UserId() ids.UserId {
	return ids.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
}

func OtherUserId() ids.UserId {
	return ids.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
}

func SeedTrack(t TB, repo *TrackRepo, userId ids.UserId, title, artist, album string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, title, artist, album, time.Now)
	if err != nil {
		t.Fatalf("seedTrack: %v", err)
	}
	repo.Seed(track)
	return track
}

func SeedReadyTrack(t TB, repo *TrackRepo, userId ids.UserId, title, artist, album, audioRef string) *domain.Track {
	t.Helper()
	track := SeedTrack(t, repo, userId, title, artist, album)
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("seedReadyTrack: %v", err)
	}
	return track
}

func SeedPlaylist(t TB, repo *PlaylistRepo, userId ids.UserId, name string) *domain.Playlist {
	t.Helper()
	playlist, err := domain.NewPlaylist(userId, name, time.Now())
	if err != nil {
		t.Fatalf("seedPlaylist: %v", err)
	}
	repo.Seed(playlist)
	return playlist
}
