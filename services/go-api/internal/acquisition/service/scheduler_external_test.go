package service_test

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/internal/adapters/memqueue"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	kernel "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

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
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, 4)
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
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, 1)
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
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, 4)
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
		upgrader := &recordingUpgrader{}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(&recordingAcquirer{}, 4,
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
		acquirer := &recordingAcquirer{}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, 1,
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

type failingAcquirer struct{}

func (failingAcquirer) Execute(context.Context, kernel.UserId, kernel.TrackId) error {
	return errors.New("every source refused the track")
}

func (failingAcquirer) ExecuteReplace(context.Context, kernel.UserId, kernel.TrackId) error {
	return errors.New("every source refused the track")
}

func (failingAcquirer) RefuseQueued(context.Context, kernel.UserId, kernel.TrackId) {}

type panicOnMessage struct {
	slog.Handler
	message string
}

func (h panicOnMessage) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == h.message {
		panic("log handler exploded on " + r.Message)
	}
	return h.Handler.Handle(ctx, r)
}

func TestScheduler_AJobWhoseCompletionPanicsAfterItIsRecordedIsRecordedOnce(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(panicOnMessage{Handler: slog.NewTextHandler(io.Discard, nil), message: "background acquisition failed"}))
	queue := &firstOutcomeQueue{Queue: memqueue.New(make(chan struct{}, 1), time.Now), outcome: make(chan jobOutcome, 1)}
	scheduler := service.NewBackgroundAcquisitionScheduler(failingAcquirer{}, 1, service.WithJobQueue(queue))
	defer scheduler.Shutdown(context.Background())
	trackID := kernel.NewTrackId()

	if err := scheduler.Schedule(context.Background(), kernel.NewUserId(uuid.New()), trackID, ""); err != nil {
		t.Fatalf("Schedule = %v, want nil", err)
	}
	<-queue.outcome

	var records []ports.JobRecord
	for _, r := range scheduler.Status().Recent {
		if r.TrackID == trackID.String() {
			records = append(records, r)
		}
	}
	if len(records) != 1 {
		t.Fatalf("recent records for the job = %+v, want exactly one", records)
	}
	if records[0].State != ports.JobFailed || records[0].Reason == "panic" {
		t.Errorf("record = %s %q, want the job's own failure, not the log handler's panic", records[0].State, records[0].Reason)
	}
}

type succeedingAcquirer struct{ failingAcquirer }

func (succeedingAcquirer) Execute(context.Context, kernel.UserId, kernel.TrackId) error { return nil }

type capturedLogs struct {
	mu      *sync.Mutex
	records *[]slog.Record
}

func (h capturedLogs) Enabled(context.Context, slog.Level) bool { return true }

func (h capturedLogs) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, r)
	return nil
}

func (h capturedLogs) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h capturedLogs) WithGroup(string) slog.Handler { return h }

func logsOfOneJob(t *testing.T, acquirer interface {
	Execute(context.Context, kernel.UserId, kernel.TrackId) error
	ExecuteReplace(context.Context, kernel.UserId, kernel.TrackId) error
	RefuseQueued(context.Context, kernel.UserId, kernel.TrackId)
},
) []slog.Record {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var records []slog.Record
	slog.SetDefault(slog.New(capturedLogs{mu: &sync.Mutex{}, records: &records}))
	queue := &firstOutcomeQueue{Queue: memqueue.New(make(chan struct{}, 1), time.Now), outcome: make(chan jobOutcome, 1)}
	scheduler := service.NewBackgroundAcquisitionScheduler(acquirer, 1, service.WithJobQueue(queue))
	if err := scheduler.Schedule(context.Background(), kernel.NewUserId(uuid.New()), kernel.NewTrackId(), ""); err != nil {
		t.Fatalf("Schedule = %v, want nil", err)
	}
	<-queue.outcome
	scheduler.Shutdown(context.Background())
	return records
}

func TestScheduler_AFailedJobIsLoggedAsAnErrorWithItsReason(t *testing.T) {
	var failed []slog.Record
	for _, r := range logsOfOneJob(t, failingAcquirer{}) {
		if r.Message == "background acquisition failed" {
			failed = append(failed, r)
		}
	}

	if len(failed) != 1 || failed[0].Level != slog.LevelError {
		t.Fatalf("failure records = %+v, want exactly one at error level", failed)
	}
	reason := ""
	failed[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "error" {
			reason = a.Value.String()
		}
		return true
	})
	if reason != "every source refused the track" {
		t.Errorf("logged error = %q, want the job's failure reason", reason)
	}
}

func TestScheduler_ASucceededJobLogsNoCompletionRecord(t *testing.T) {
	for _, r := range logsOfOneJob(t, succeedingAcquirer{}) {
		if r.Message == "" || r.Message == "background acquisition failed" || r.Message == "background acquisition will retry" {
			t.Errorf("record %q at %v, want no completion record for a job that succeeded", r.Message, r.Level)
		}
	}
}

func TestScheduler_ShutdownWaitsForAnInFlightJobToFinishAndDoesNotCancelIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acquirer := &holdingAcquirer{release: make(chan struct{})}
		scheduler := acquisition.NewBackgroundAcquisitionScheduler(acquirer, 1)
		if err := scheduler.Schedule(context.Background(), kernel.NewUserId(uuid.New()), kernel.NewTrackId(), ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}
		synctest.Wait()

		returned := make(chan struct{})
		go func() {
			scheduler.Shutdown(context.Background())
			close(returned)
		}()
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("Shutdown returned while a job was still in flight, want it to drain first")
		default:
		}

		close(acquirer.release)
		synctest.Wait()
		select {
		case <-returned:
		default:
			t.Fatal("Shutdown did not return after the in-flight job finished")
		}
		if status := scheduler.Status(); status.Succeeded != 1 || status.Failed != 0 {
			t.Errorf("succeeded = %d, failed = %d, want the job to finish normally (1, 0), not be cancelled", status.Succeeded, status.Failed)
		}
	})
}
