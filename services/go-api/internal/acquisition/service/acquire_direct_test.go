package service

import (
	"context"
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func TestIsDirectAcquireURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"soundcloud permalink", "https://soundcloud.com/liltecca/fell-in-love", true},
		{"soundcloud subdomain", "https://m.soundcloud.com/x/leak", true},
		{"empty", "", false},
		{"deezer not downloadable", "https://www.deezer.com/track/123", false},
		{"youtube not a discovery source", "https://youtube.com/watch?v=abc", false},
		{"garbage", "not a url ::::", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDirectAcquireURL(tt.url); got != tt.want {
				t.Errorf("isDirectAcquireURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

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

func TestExecute_DirectSoundCloudURL_SkipsSearch(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)

	// searchResults is empty: if Execute fell back to search, selection would fail
	// and the track would never go ready. downloadPath non-empty so the direct
	// Download→Tag→Store→Update chain succeeds.
	searcher := &fakeAudioSearcher{downloadPath: "/tmp/altune/fell-in-love.mp3"}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	scURL := "https://soundcloud.com/liltecca/fell-in-love"
	if err := svc.Execute(context.Background(), userId, track.ID, scURL); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Fatalf("status = %v, want ready (direct download)", updated.AcquisitionStatus)
	}
	if searcher.searchCalled {
		t.Error("search must be skipped when a direct SoundCloud URL is supplied")
	}
	if len(searcher.downloadURLs) != 1 || searcher.downloadURLs[0] != scURL {
		t.Errorf("direct download should use the exact SoundCloud URL, got %v", searcher.downloadURLs)
	}
}

func TestExecute_NoSourceURL_UsesSearch(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := pendingTrack(t, repo, userId)

	// No source URL → search path. Empty results → selection fails → track failed,
	// but the point is that Search WAS consulted (no direct shortcut).
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	_ = svc.Execute(context.Background(), userId, track.ID, "")

	if !searcher.searchCalled {
		t.Error("expected the search pipeline to run when no source URL is supplied")
	}
}
