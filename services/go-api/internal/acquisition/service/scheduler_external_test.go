package service_test

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/service"
	kernel "altune/go-api/internal/shared/kernel"
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/google/uuid"
)

type ranTracks struct {
	mu  sync.Mutex
	ran []kernel.TrackId
}

func (r *ranTracks) record(id kernel.TrackId) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, id)
}

func (r *ranTracks) has(id kernel.TrackId) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.ran, id)
}

type recordingAcquirer struct{ ranTracks }

func (a *recordingAcquirer) Execute(_ context.Context, _ kernel.UserId, id kernel.TrackId) error {
	a.record(id)
	return nil
}

func (a *recordingAcquirer) ExecuteReplace(context.Context, kernel.UserId, kernel.TrackId) error {
	return nil
}

func (a *recordingAcquirer) RefuseQueued(context.Context, kernel.UserId, kernel.TrackId) {}

func (r *ranTracks) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ran)
}

type recordingUpgrader struct{ ranTracks }

func (u *recordingUpgrader) ExecuteUpgrade(_ context.Context, _ kernel.UserId, id kernel.TrackId) error {
	u.record(id)
	return nil
}

type holdingAcquirer struct {
	ranTracks
	release chan struct{}
}

func (a *holdingAcquirer) Execute(_ context.Context, _ kernel.UserId, id kernel.TrackId) error {
	a.record(id)
	<-a.release
	return nil
}

func (a *holdingAcquirer) ExecuteReplace(context.Context, kernel.UserId, kernel.TrackId) error {
	return nil
}

func (a *holdingAcquirer) RefuseQueued(context.Context, kernel.UserId, kernel.TrackId) {}

func TestScheduler_ABurstOfAddsStartsOnEveryIdleAcquireWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var wg sync.WaitGroup
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, &wg, make(chan struct{}, 4))
		defer scheduler.Shutdown(context.Background())
		defer close(acquirer.release)
		user := kernel.NewUserId(uuid.New())

		synctest.Wait()
		for range 4 {
			if err := scheduler.Schedule(context.Background(), user, kernel.NewTrackId(), ""); err != nil {
				t.Fatalf("Schedule = %v, want nil", err)
			}
		}
		synctest.Wait()

		if got := acquirer.count(); got != 4 {
			t.Fatalf("%d of 4 adds started before the poll interval, want every idle acquire worker to take one", got)
		}
	})
}

func TestScheduler_ResumeRunsAJobThatWaitedWhilePaused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var wg sync.WaitGroup
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, &wg, make(chan struct{}, 1))
		defer scheduler.Shutdown(context.Background())
		user := kernel.NewUserId(uuid.New())
		running, waiting := kernel.NewTrackId(), kernel.NewTrackId()

		synctest.Wait()
		for _, track := range []kernel.TrackId{running, waiting} {
			if err := scheduler.Schedule(context.Background(), user, track, ""); err != nil {
				t.Fatalf("Schedule(%s) = %v, want nil", track, err)
			}
			synctest.Wait()
		}
		scheduler.Pause()
		close(acquirer.release)
		synctest.Wait()
		if acquirer.has(waiting) {
			t.Fatalf("the waiting add of %s ran while the scheduler was paused", waiting)
		}

		scheduler.Resume()
		synctest.Wait()

		if !acquirer.has(waiting) {
			t.Fatalf("the waiting add of %s did not run before the poll interval after Resume", waiting)
		}
	})
}

func TestScheduler_ResumeStartsTheWholeBacklogOnEveryIdleWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var wg sync.WaitGroup
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, &wg, make(chan struct{}, 4))
		defer scheduler.Shutdown(context.Background())
		defer close(acquirer.release)
		user := kernel.NewUserId(uuid.New())

		synctest.Wait()
		for range 8 {
			if err := scheduler.Schedule(context.Background(), user, kernel.NewTrackId(), ""); err != nil {
				t.Fatalf("Schedule = %v, want nil", err)
			}
			synctest.Wait()
		}
		scheduler.Pause()
		for range 4 {
			acquirer.release <- struct{}{}
		}
		synctest.Wait()
		if got := acquirer.count(); got != 4 {
			t.Fatalf("%d of 8 adds started before Resume, want the 4 that held the workers and none while paused", got)
		}

		scheduler.Resume()
		synctest.Wait()

		if got := acquirer.count(); got != 8 {
			t.Fatalf("%d of 8 adds started after Resume, want the 4 backlogged adds on the 4 idle workers before the poll interval", got)
		}
	})
}

func TestScheduler_AnUpgradeRunsAtOnceWhileAcquireWorkersAreIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var wg sync.WaitGroup
		upgrader := &recordingUpgrader{}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(&recordingAcquirer{}, &wg, make(chan struct{}, 4),
			acquisition.WithUpgradeLane(upgrader, 1))
		defer scheduler.Shutdown(context.Background())
		user := kernel.NewUserId(uuid.New())

		for round := range 3 {
			synctest.Wait()
			track := kernel.NewTrackId()
			if err := scheduler.ScheduleUpgrade(context.Background(), user, track); err != nil {
				t.Fatalf("round %d: ScheduleUpgrade = %v, want nil", round, err)
			}
			synctest.Wait()
			if !upgrader.has(track) {
				t.Fatalf("round %d: the upgrade of %s did not run before the poll interval; an idle acquire worker took its wake-up", round, track)
			}
		}
	})
}

func TestScheduler_AnAddRunsAtOnceWhileUpgradeWorkersAreIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var wg sync.WaitGroup
		acquirer := &recordingAcquirer{}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, &wg, make(chan struct{}, 1),
			acquisition.WithUpgradeLane(&recordingUpgrader{}, 4))
		defer scheduler.Shutdown(context.Background())
		user := kernel.NewUserId(uuid.New())

		for round := range 3 {
			synctest.Wait()
			track := kernel.NewTrackId()
			if err := scheduler.Schedule(context.Background(), user, track, ""); err != nil {
				t.Fatalf("round %d: Schedule = %v, want nil", round, err)
			}
			synctest.Wait()
			if !acquirer.has(track) {
				t.Fatalf("round %d: the add of %s did not run before the poll interval; an idle upgrade worker took its wake-up", round, track)
			}
		}
	})
}

func TestScheduler_ASelectOutageOnTheFinalRunIsNotRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		track := acquisitiontest.LedgerTrack{TrackSnapshot: pendingBlindingLights()}
		ledger := acquisitiontest.NewLedger(track)
		svc := acquireService(ledger, service.NewSourceRegistry(throttled(), healthyButRejected()), &objectStore{objects: map[string]string{}})

		outcome := runOneJobAs(t, svc, track, 3)

		if outcome.released {
			t.Errorf("outcome = %+v, want the final run settled, not retried", outcome)
		}
		if code := persistedCode(t, ledger, track.ID); code != kernel.FailureSourceUnavailable {
			t.Errorf("persisted failure code = %q, want source_unavailable", code)
		}
	})
}
