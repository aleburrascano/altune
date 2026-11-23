package service_test

import (
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/internal/adapters/tempfs"
	acqdomain "altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type objectStore struct {
	mu        sync.Mutex
	objects   map[string]string
	storeErr  error
	deleteErr error
}

func (s *objectStore) Exists(_ context.Context, audioRef string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[audioRef]
	return ok, nil
}

func (s *objectStore) Store(_ context.Context, sourcePath string, audioRef string) error {
	if s.storeErr != nil {
		return s.storeErr
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[audioRef] = string(data)
	return nil
}

func (s *objectStore) Delete(_ context.Context, audioRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, audioRef)
	return nil
}

func (s *objectStore) holds(audioRef string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[audioRef]
	return ok
}

type freshAudioSource struct{}

func (freshAudioSource) Name() acqdomain.SourceName { return "new-audio" }

func (freshAudioSource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{{
		Title:      "Blinding Lights",
		URL:        "https://www.youtube.com/watch?v=freshAAAAAA",
		Channel:    "The Weeknd - Topic",
		Categories: []string{"Music"},
	}}, nil
}

func (freshAudioSource) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return ports.FetchedAudio{Path: path}, os.WriteFile(path, []byte("new bytes"), 0o600)
}

type servedTrack struct {
	ledger      *acquisitiontest.Ledger
	store       *objectStore
	userID      shared.UserId
	trackID     shared.TrackId
	originalRef string
}

func seedReadyTrack(t *testing.T) servedTrack {
	t.Helper()
	userID, trackID := shared.NewUserId(uuid.New()), shared.NewTrackId()
	originalRef := service.BuildAudioRef(service.TrackRef{
		ID: trackID.String(), UserID: userID.String(),
		Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
	}, "audio.mp3")
	sourceURL := "https://www.youtube.com/watch?v=currentAAAA"
	ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
		ID: trackID, UserId: userID, Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
		Status: ports.TrackReady, AudioRef: &originalRef, AudioSourceURL: &sourceURL,
	}})
	store := &objectStore{objects: map[string]string{originalRef: "original bytes"}}
	return servedTrack{ledger: ledger, store: store, userID: userID, trackID: trackID, originalRef: originalRef}
}

func (r servedTrack) service(opts ...func(*service.AcquireTrackAudioService)) *service.AcquireTrackAudioService {
	opts = append([]func(*service.AcquireTrackAudioService){service.WithAcquireTempWorkspace(tempfs.New())}, opts...)
	return service.NewAcquireTrackAudioService(r.ledger, service.NewSourceRegistry(freshAudioSource{}), r.store, opts...)
}
