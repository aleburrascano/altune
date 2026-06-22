package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// blockingRepo holds the first GetByID inside the goroutine (signaling started,
// then waiting on release) so the inflight-dedup window can be tested
// deterministically. It returns (nil, nil) so Execute exits quickly once freed.
type blockingRepo struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (r *blockingRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	r.calls.Add(1)
	close(r.started)
	<-r.release
	return nil, nil
}
func (r *blockingRepo) Update(_ context.Context, _ *domain.Track) error { return nil }

func TestBackgroundScheduler_Schedule_DedupsInflight(t *testing.T) {
	repo := &blockingRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, &fakeAudioSearcher{}, newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	scheduler.Schedule(userId, trackId, "")
	<-repo.started                          // first goroutine is in-flight (inflight key set)
	scheduler.Schedule(userId, trackId, "") // same track → must be deduped (skipped)
	close(repo.release)
	wg.Wait()

	if got := repo.calls.Load(); got != 1 {
		t.Errorf("GetByID calls = %d, want 1 (second schedule must be deduped)", got)
	}
}

type countingRepo struct{ calls atomic.Int32 }

func (r *countingRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	r.calls.Add(1)
	return nil, nil
}
func (r *countingRepo) Update(_ context.Context, _ *domain.Track) error { return nil }

func TestBackgroundScheduler_Schedule_AfterShutdown_NoOp(t *testing.T) {
	repo := &countingRepo{}
	svc := NewAcquireTrackAudioService(repo, &fakeAudioSearcher{}, newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler.Shutdown(ctx)

	scheduler.Schedule(shared.NewUserId(uuid.New()), domain.NewTrackId(), "")
	wg.Wait()

	if got := repo.calls.Load(); got != 0 {
		t.Errorf("GetByID calls = %d, want 0 (schedule after shutdown must be a no-op)", got)
	}
}

type panicRepo struct{}

func (r *panicRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	panic("boom")
}
func (r *panicRepo) Update(_ context.Context, _ *domain.Track) error { return nil }

// A panic inside an acquisition goroutine must be recovered, not crash the
// process; the WaitGroup must still drain.
func TestBackgroundScheduler_Schedule_RecoversFromPanic(t *testing.T) {
	svc := NewAcquireTrackAudioService(&panicRepo{}, &fakeAudioSearcher{}, newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	scheduler.Schedule(shared.NewUserId(uuid.New()), domain.NewTrackId(), "")
	wg.Wait() // returns only if the panic was recovered and wg.Done ran
}
