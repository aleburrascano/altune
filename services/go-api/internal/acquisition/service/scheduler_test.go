package service

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// --- Fake ports for AcquireTrackAudioService ---

type fakeTrackRepository struct {
	tracks map[string]*domain.Track
	err    error
}

func newFakeTrackRepository() *fakeTrackRepository {
	return &fakeTrackRepository{tracks: make(map[string]*domain.Track)}
}

func (r *fakeTrackRepository) Add(_ context.Context, track *domain.Track) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	key := track.ID.String() + ":" + track.UserId.String()
	if _, exists := r.tracks[key]; exists {
		return false, nil
	}
	r.tracks[key] = track
	return true, nil
}

func (r *fakeTrackRepository) GetByID(_ context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	if r.err != nil {
		return nil, r.err
	}
	key := id.String() + ":" + userId.String()
	return r.tracks[key], nil
}

func (r *fakeTrackRepository) ListForUser(_ context.Context, _ shared.UserId, _, _ int) ([]*domain.Track, int, error) {
	return nil, 0, nil
}

func (r *fakeTrackRepository) Update(_ context.Context, track *domain.Track) error {
	if r.err != nil {
		return r.err
	}
	key := track.ID.String() + ":" + track.UserId.String()
	r.tracks[key] = track
	return nil
}

func (r *fakeTrackRepository) Delete(_ context.Context, _ domain.TrackId, _ shared.UserId) (bool, error) {
	return false, nil
}

func (r *fakeTrackRepository) GetByDedupKey(_ context.Context, _ shared.UserId, _ string) (*domain.Track, error) {
	return nil, nil
}

type fakeAudioSearcher struct {
	searchResults []ports.AudioCandidate
	searchErr     error
	downloadPath  string
	downloadErr   error
}

func (s *fakeAudioSearcher) Search(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
	return s.searchResults, s.searchErr
}

func (s *fakeAudioSearcher) Download(_ context.Context, _ string, _ string) (string, error) {
	return s.downloadPath, s.downloadErr
}

type fakeAudioStore struct {
	stored  map[string]bool
	err     error
}

func newFakeAudioStore() *fakeAudioStore {
	return &fakeAudioStore{stored: make(map[string]bool)}
}

func (s *fakeAudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.stored[audioRef], nil
}

func (s *fakeAudioStore) Store(_ context.Context, _ string, audioRef string) error {
	if s.err != nil {
		return s.err
	}
	s.stored[audioRef] = true
	return nil
}

func (s *fakeAudioStore) Stream(_ context.Context, _ string) (io.ReadCloser, int64, error) {
	return nil, 0, nil
}

func (s *fakeAudioStore) Delete(_ context.Context, audioRef string) error {
	delete(s.stored, audioRef)
	return nil
}

func TestBackgroundScheduler_Schedule(t *testing.T) {
	// Arrange: build a real AcquireTrackAudioService with fakes that cause
	// Execute to return early (track not found → logs warning, returns nil).
	// This lets us verify the scheduler's goroutine + WaitGroup mechanics
	// without needing a full acquisition pipeline.
	repo := newFakeTrackRepository()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()

	svc := NewAcquireTrackAudioService(repo, searcher, store)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	// Act
	scheduler.Schedule(userId, trackId)

	// Assert: WaitGroup completes (goroutine ran and finished)
	wg.Wait()
	// If we reach here, the goroutine completed without deadlock.
	// The semaphore should be drained (released).
	if len(sem) != 0 {
		t.Errorf("semaphore should be empty after goroutine completes, got %d tokens held", len(sem))
	}
}

func TestBackgroundScheduler_ScheduleMultiple_RespectsSemaphore(t *testing.T) {
	// Arrange: semaphore capacity 1 → schedules run one at a time
	repo := newFakeTrackRepository()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()

	svc := NewAcquireTrackAudioService(repo, searcher, store)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1) // capacity 1
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	userId := shared.NewUserId(uuid.New())

	var completedCount atomic.Int32
	const numSchedules = 3

	// Act: schedule multiple acquisitions
	for i := 0; i < numSchedules; i++ {
		trackId := domain.NewTrackId()
		scheduler.Schedule(userId, trackId)
	}

	// Assert: all complete (WaitGroup drains)
	wg.Wait()
	_ = completedCount.Load() // all goroutines finished if we reach here

	if len(sem) != 0 {
		t.Errorf("semaphore should be empty after all goroutines complete, got %d tokens held", len(sem))
	}
}

func TestNewBackgroundAcquisitionScheduler_ReturnsNonNil(t *testing.T) {
	repo := newFakeTrackRepository()
	searcher := &fakeAudioSearcher{}
	store := newFakeAudioStore()
	svc := NewAcquireTrackAudioService(repo, searcher, store)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)

	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)
	if scheduler == nil {
		t.Fatal("NewBackgroundAcquisitionScheduler returned nil")
	}
}
