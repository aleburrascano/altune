package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// TestBackgroundScheduler_OnePrincipalCannotStarveAnother reproduces the
// starvation defect: with only a single shared admission queue, one authenticated
// user can fill every slot and every other user's job is rejected. A per-principal
// share must cap how much of the queue one user holds so slots remain for others.
//
// Setup: global queue depth 4, per-principal cap 2, worker concurrency 1 (so the
// first admitted job stays in flight and nothing drains). User A bursts 4 jobs;
// with fairness only 2 are admitted, leaving 2 global slots for user B. Without
// the fix, A's 4 jobs fill the whole queue and B is refused — the regression.
func TestBackgroundScheduler_OnePrincipalCannotStarveAnother(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem,
		WithQueueDepth(4), WithPrincipalQueueDepth(2))

	userA := shared.NewUserId(uuid.New())
	userB := shared.NewUserId(uuid.New())

	// User A floods: first 2 admitted (its full share), the rest rejected as its
	// per-principal queue is full — not as a global queue-full shed.
	results := make([]error, 4)
	for i := range results {
		results[i] = scheduler.Schedule(context.Background(), userA, domain.NewTrackId(), "")
	}
	for i := 0; i < 2; i++ {
		if results[i] != nil {
			t.Fatalf("user A schedule #%d = %v, want nil (within per-principal share)", i+1, results[i])
		}
	}
	for i := 2; i < 4; i++ {
		if !errors.Is(results[i], ErrPrincipalQueueFull) {
			t.Fatalf("user A schedule #%d = %v, want ErrPrincipalQueueFull (share exhausted)", i+1, results[i])
		}
	}

	// A worker for user A is now in flight and holding the sole semaphore slot.
	<-repo.started

	// User B must still get in: A holds only its 2-slot share, so 2 global slots
	// remain. Before the fix, A owned all 4 slots and this was ErrAcquisitionQueueFull.
	if err := scheduler.Schedule(context.Background(), userB, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("user B schedule while A floods = %v, want nil (A must not starve B)", err)
	}

	close(repo.release)
	wg.Wait()
}

// TestBackgroundScheduler_PrincipalShareReleasesOnCompletion pins that a
// principal's share is refunded when its jobs finish: a user capped at one slot
// can schedule again once the prior job drains. A leaked reservation would keep
// the user permanently rejected.
func TestBackgroundScheduler_PrincipalShareReleasesOnCompletion(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	close(repo.release) // jobs complete immediately
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1),
		WithQueueDepth(4), WithPrincipalQueueDepth(1))

	user := shared.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("first schedule = %v, want nil", err)
	}
	wg.Wait() // let the job drain and refund the principal slot

	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("second schedule after drain = %v, want nil (share must be refunded)", err)
	}
	wg.Wait()
}
