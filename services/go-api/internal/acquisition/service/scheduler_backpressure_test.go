package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// burstRepo blocks the first in-flight acquisition until release is closed,
// letting a test saturate the worker semaphore and observe how many jobs pile
// up behind it. sync.Once guards the started signal so repeated GetByID calls
// (one per admitted job once drained) do not double-close the channel.
type burstRepo struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (r *burstRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	r.calls.Add(1)
	r.once.Do(func() { close(r.started) })
	<-r.release
	return nil, nil
}

func (r *burstRepo) Update(_ context.Context, _ *domain.Track) error { return nil }

// TestBackgroundScheduler_BoundsQueueDepthUnderBurst reproduces the
// backpressure defect: a burst of Schedule calls far beyond the configured
// worker concurrency must not register a job-log entry (and spawn a goroutine)
// per arrival. Total outstanding work has to stay bounded relative to
// concurrency, not grow with arrivals.
func TestBackgroundScheduler_BoundsQueueDepthUnderBurst(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	userId := shared.NewUserId(uuid.New())
	const burst = 60
	for i := 0; i < burst; i++ {
		scheduler.Schedule(userId, domain.NewTrackId(), "")
	}

	// Ensure a worker is actually in-flight so the semaphore is saturated.
	<-repo.started

	active := len(scheduler.Status().ActiveJobs)
	maxAllowed := cap(sem) * 8
	if active > maxAllowed {
		t.Errorf("active job-log entries = %d under burst of %d; want bounded <= %d (arrivals must not spawn unbounded work)",
			active, burst, maxAllowed)
	}

	close(repo.release)
	wg.Wait()
}

// TestBackgroundScheduler_ReportsQueueFull pins the reporting-full contract: at
// a fixed queue depth, a burst admits exactly depth jobs and reports the rest
// as rejected, without registering job-log entries for them.
func TestBackgroundScheduler_ReportsQueueFull(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	const queueDepth = 4
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem, WithQueueDepth(queueDepth))

	userId := shared.NewUserId(uuid.New())
	const burst = 50
	for i := 0; i < burst; i++ {
		scheduler.Schedule(userId, domain.NewTrackId(), "")
	}

	<-repo.started

	status := scheduler.Status()
	if got := len(status.ActiveJobs); got != queueDepth {
		t.Errorf("active job-log entries = %d, want exactly %d (queue depth)", got, queueDepth)
	}
	if want := uint64(burst - queueDepth); status.Rejected != want {
		t.Errorf("rejected = %d, want %d (arrivals past the queue depth)", status.Rejected, want)
	}

	close(repo.release)
	wg.Wait()
}
