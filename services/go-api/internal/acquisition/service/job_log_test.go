package service_test

import (
	"altune/go-api/internal/acquisition"
	kernel "altune/go-api/internal/shared/kernel"
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

const acquireTakes = 2 * time.Second

type slowAcquirer struct{}

func (slowAcquirer) Execute(context.Context, kernel.UserId, kernel.TrackId) error {
	time.Sleep(acquireTakes)
	return nil
}

func (slowAcquirer) ExecuteReplace(context.Context, kernel.UserId, kernel.TrackId) error {
	return nil
}

func (slowAcquirer) RefuseQueued(context.Context, kernel.UserId, kernel.TrackId) {}

func TestStatus_ARunningJobReportsTheTimeSinceItWasScheduled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var wg sync.WaitGroup
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, &wg, make(chan struct{}, 1))
		defer scheduler.Shutdown(context.Background())
		defer close(acquirer.release)
		track := kernel.NewTrackId()
		if err := scheduler.Schedule(context.Background(), kernel.NewUserId(uuid.New()), track, ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}
		synctest.Wait()

		time.Sleep(3 * time.Second)

		active := scheduler.Status().ActiveJobs
		if len(active) != 1 || active[0].TrackID != track.String() || active[0].State != acquisition.JobRunning {
			t.Fatalf("ActiveJobs = %+v, want the running job of %s", active, track)
		}
		if got := active[0].ElapsedMs; got != 3000 {
			t.Errorf("ElapsedMs = %d, want 3000: the time since the job was scheduled", got)
		}
	})
}

func TestStatus_AFinishedJobReportsHowLongItRan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var wg sync.WaitGroup
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(slowAcquirer{}, &wg, make(chan struct{}, 1))
		defer scheduler.Shutdown(context.Background())
		track := kernel.NewTrackId()
		if err := scheduler.Schedule(context.Background(), kernel.NewUserId(uuid.New()), track, ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}

		time.Sleep(acquireTakes)
		synctest.Wait()

		status := scheduler.Status()
		if len(status.Recent) != 1 || status.Recent[0].TrackID != track.String() || status.Recent[0].State != acquisition.JobSucceeded {
			t.Fatalf("Recent = %+v, want the succeeded job of %s", status.Recent, track)
		}
		if got := status.Recent[0].ElapsedMs; got != acquireTakes.Milliseconds() {
			t.Errorf("ElapsedMs = %d, want %d: how long the acquire ran", got, acquireTakes.Milliseconds())
		}
		if status.Succeeded != 1 || len(status.ActiveJobs) != 0 {
			t.Errorf("Succeeded = %d, ActiveJobs = %+v, want 1 and none", status.Succeeded, status.ActiveJobs)
		}
	})
}
