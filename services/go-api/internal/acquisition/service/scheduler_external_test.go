package service_test

import (
	"altune/go-api/internal/acquisition"
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

type recordingUpgrader struct{ ranTracks }

func (u *recordingUpgrader) ExecuteUpgrade(_ context.Context, _ kernel.UserId, id kernel.TrackId) error {
	u.record(id)
	return nil
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
