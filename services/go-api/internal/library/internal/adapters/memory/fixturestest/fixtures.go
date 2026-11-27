package fixturestest

import (
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/ids"
	"time"

	"github.com/google/uuid"
)

var randomIDs ids.Source = ids.Real

func fixedUserId(fill byte) ids.UserId {
	var id uuid.UUID
	for i := range id {
		id[i] = fill
	}
	return ids.NewUserId(id)
}

func UserId() ids.UserId { return fixedUserId(0xaa) }

func OtherUserId() ids.UserId { return fixedUserId(0xbb) }

func SeedTrack(t memory.TB, repo *memory.TrackRepo, userId ids.UserId, title, artist, album string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(domain.NewTrackInput{UserId: userId, Title: title, Artist: artist, Album: album}, time.Now, randomIDs)
	if err != nil {
		t.Fatalf("seedTrack: %v", err)
	}
	repo.Seed(track)
	return track
}

func SeedReadyTrack(t memory.TB, repo *memory.TrackRepo, userId ids.UserId, title, artist, album, audioRef string) *domain.Track {
	t.Helper()
	track := SeedTrack(t, repo, userId, title, artist, album)
	if err := track.MarkReady(audioRef, randomIDs); err != nil {
		t.Fatalf("seedReadyTrack: %v", err)
	}
	return track
}

func SeedPlaylist(t memory.TB, repo *memory.PlaylistRepo, userId ids.UserId, name string) *domain.Playlist {
	t.Helper()
	playlist, err := domain.NewPlaylist(userId, name, time.Now(), randomIDs)
	if err != nil {
		t.Fatalf("seedPlaylist: %v", err)
	}
	repo.Seed(playlist)
	return playlist
}
