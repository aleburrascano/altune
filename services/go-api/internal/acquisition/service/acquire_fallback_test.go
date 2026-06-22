package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// flakyDownloadSearcher fails the first Download (the direct attempt) and
// succeeds afterwards (the search attempt), with Search returning one strong
// Topic-channel candidate.
type flakyDownloadSearcher struct {
	downloadCalls int
	searchCalled  bool
}

func (s *flakyDownloadSearcher) Search(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
	s.searchCalled = true
	return []ports.AudioCandidate{
		{
			Title:        "Fell In Love",
			Artist:       "Lil Tecca",
			DurationSecs: 150,
			URL:          "https://youtube.com/watch?v=topic",
			Channel:      "Lil Tecca - Topic",
			Categories:   []string{"Music"},
			ViewCount:    1_000_000,
		},
	}, nil
}

func (s *flakyDownloadSearcher) Download(_ context.Context, _, outDir string) (string, error) {
	s.downloadCalls++
	if s.downloadCalls == 1 {
		return "", errors.New("direct download failed")
	}
	return outDir + "/track.mp3", nil
}

func TestExecute_DirectFails_FallsBackToSearch(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track, err := domain.NewTrack(userId, "Fell In Love", "Lil Tecca", "")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	searcher := &flakyDownloadSearcher{}
	svc := NewAcquireTrackAudioService(repo, searcher, newFakeAudioStore())

	scURL := "https://soundcloud.com/liltecca/fell-in-love"
	if err := svc.Execute(context.Background(), userId, track.ID, scURL); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !searcher.searchCalled {
		t.Error("expected fallback to the search pipeline after the direct download failed")
	}
	if searcher.downloadCalls < 2 {
		t.Errorf("expected a second download via the search fallback, got %d download calls", searcher.downloadCalls)
	}
	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want ready after fallback", updated.AcquisitionStatus)
	}
}

func TestIsDirectAcquireURL_RejectsSets(t *testing.T) {
	if isDirectAcquireURL("https://soundcloud.com/liltecca/sets/leaks") {
		t.Error("SoundCloud set URLs must not use the direct path (they download multiple tracks)")
	}
}
