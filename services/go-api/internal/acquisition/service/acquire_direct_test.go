package service

import (
	"context"
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// pendingTrack stores a fresh (pending) track in the repo and returns it.
func pendingTrack(t *testing.T, repo *fakeTrackRepository, userId shared.UserId) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Fell In Love", "Lil Tecca", "")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	return track
}

// Acquisition always uses the search pipeline now (the direct SoundCloud path was
// removed because SoundCloud's public stream is often a ~30s preview).
func TestExecute_AlwaysSearches(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)

	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	if !searcher.searchCalled {
		t.Error("expected the search pipeline to run")
	}
	if len(searcher.downloadURLs) != 0 {
		t.Errorf("no direct download should occur; got download URLs %v", searcher.downloadURLs)
	}
}
