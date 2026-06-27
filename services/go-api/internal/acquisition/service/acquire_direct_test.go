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
// removed because SoundCloud's public stream is often a ~30s preview). A non-empty
// sourceURL is ignored — Search is still consulted.
func TestExecute_IgnoresSourceURL_AlwaysSearches(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)

	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	// Even with a SoundCloud URL supplied, Execute must search rather than download
	// the URL directly.
	_ = svc.Execute(context.Background(), userId, track.ID, "https://soundcloud.com/liltecca/fell-in-love")

	if !searcher.searchCalled {
		t.Error("expected the search pipeline to run regardless of source URL")
	}
	if len(searcher.downloadURLs) != 0 {
		t.Errorf("no direct download should occur; got download URLs %v", searcher.downloadURLs)
	}
}

func TestExecute_NoSourceURL_UsesSearch(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)

	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	_ = svc.Execute(context.Background(), userId, track.ID, "")

	if !searcher.searchCalled {
		t.Error("expected the search pipeline to run when no source URL is supplied")
	}
}
