package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"testing"

	"github.com/google/uuid"
)

func testUserId() shared.UserId {
	return shared.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
}

func seedTrack(t *testing.T, repo *catalogtest.TrackRepo, userId shared.UserId, title, artist, album string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, title, artist, album)
	if err != nil {
		t.Fatalf("seedTrack: %v", err)
	}
	repo.Seed(track)
	return track
}

func seedReadyTrack(t *testing.T, repo *catalogtest.TrackRepo, userId shared.UserId, title, artist, album, audioRef string) *domain.Track {
	t.Helper()
	track := seedTrack(t, repo, userId, title, artist, album)
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("seedReadyTrack: %v", err)
	}
	return track
}

func seedPlaylist(t *testing.T, repo *catalogtest.PlaylistRepo, userId shared.UserId, name string) *domain.Playlist {
	t.Helper()
	playlist, err := domain.NewPlaylist(userId, name)
	if err != nil {
		t.Fatalf("seedPlaylist: %v", err)
	}
	repo.Seed(playlist)
	return playlist
}

func ptrStatus(s domain.AcquisitionStatus) *domain.AcquisitionStatus {
	return &s
}
