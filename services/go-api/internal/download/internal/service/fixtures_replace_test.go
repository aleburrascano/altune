package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/random"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func (freshAudioSource) Name() domain.SourceName { return "new-audio" }

func (freshAudioSource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{downloadtest.Candidate(
		"https://www.youtube.com/watch?v=freshAAAAAA",
		downloadtest.WithTitle("Blinding Lights"),
		downloadtest.WithChannel("The Weeknd - Topic"),
		downloadtest.WithCategories([]string{"Music"}),
	)}, nil
}

func (freshAudioSource) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return ports.FetchedAudio{Path: path}, os.WriteFile(path, []byte("new bytes"), 0o600)
}

type servedTrack struct {
	ledger      *memory.Ledger
	store       *objectStore
	userID      ids.UserId
	trackID     ids.TrackId
	originalRef string
}

func seedReadyTrack(t *testing.T) servedTrack {
	t.Helper()
	userID, trackID := ids.NewUserId(uuid.New()), ids.NewTrackId()
	originalRef := service.BuildAudioRef(service.TrackRef{
		ID: trackID.String(), UserID: userID.String(),
		Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
	}, "audio.mp3")
	sourceURL := "https://www.youtube.com/watch?v=currentAAAA"
	ledger := memory.NewLedger(memory.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
		ID: trackID, UserId: userID, Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
		Status: ports.TrackReady, Streamable: true, AudioRef: &originalRef, AudioSourceURL: &sourceURL,
	}})
	store := &objectStore{objects: map[string]string{originalRef: "original bytes"}}
	return servedTrack{ledger: ledger, store: store, userID: userID, trackID: trackID, originalRef: originalRef}
}

func (r servedTrack) service(opts ...func(*service.AcquireTrackAudioService)) *acquisitionServices {
	return r.serviceStaged(noStages(deletingFrom(r.store)), opts...)
}

type audioDeleter interface {
	Delete(ctx context.Context, audioRef string) error
}

func deletingFrom(store audioDeleter) stageMod {
	return func(s *service.StagePorts) { s.Orphans = service.MustDirectDeleteOrphans(store) }
}

func (r servedTrack) serviceStaged(stages service.StagePorts, opts ...func(*service.AcquireTrackAudioService)) *acquisitionServices {
	return mustAcquisitionServices(r.ledger, service.NewSourceRegistry(freshAudioSource{}), r.store, scratchfiles.New(), noVerification(), stages, opts...)
}

type stageMod func(*service.StagePorts)

func noStages(mods ...stageMod) service.StagePorts {
	stages := service.StagePorts{
		Prober:        downloadtest.SilentAudioProber(),
		Tagger:        ports.NoopAudioTagger(),
		Rejections:    ports.NoopRejectionStore(),
		StoredAudio:   ports.NoopStoredAudioReader(),
		Orphans:       service.MustDirectDeleteOrphans(&objectStore{objects: map[string]string{}}),
		Skips:         ports.NoopVerifySkipRecorder(),
		Events:        events.NoopPublisher(),
		DownloadLimit: service.Unlimited(),
		Observer:      service.NoObserver(),
		Sleep:         clock.Sleep,
		Now:           time.Now,
		Unit:          random.Real,
	}
	for _, mod := range mods {
		mod(&stages)
	}
	return stages
}

func probedBy(p ports.AudioProber) stageMod { return func(s *service.StagePorts) { s.Prober = p } }

func taggedBy(t ports.AudioTagger) stageMod { return func(s *service.StagePorts) { s.Tagger = t } }

func rejectingInto(r ports.RejectionStore) stageMod {
	return func(s *service.StagePorts) { s.Rejections = r }
}

func readingStoredFrom(r ports.StoredAudioReader) stageMod {
	return func(s *service.StagePorts) { s.StoredAudio = r }
}

func orphaningInto(q ports.OrphanedAudioRecorder) stageMod {
	return func(s *service.StagePorts) { s.Orphans = q }
}

func noVerification() service.VerificationPorts {
	return service.VerificationPorts{
		Identifier:      ports.NoopAudioIdentifier(),
		RecordingLookup: ports.NoopRecordingLookup(),
		Rerecordings:    ports.NoopRerecordingFinder(),
		Previews:        ports.NoopPreviewMatcher(),
		Recordings:      ports.NoopRecordingResolver(),
	}
}

func verifiedBy(identifier ports.AudioIdentifier) service.VerificationPorts {
	wiring := noVerification()
	wiring.Identifier = identifier
	return wiring
}

func resolving(resolver ports.RecordingResolver, wiring service.VerificationPorts) service.VerificationPorts {
	wiring.Recordings = resolver
	return wiring
}
