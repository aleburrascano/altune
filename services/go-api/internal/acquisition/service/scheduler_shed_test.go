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

// scheduleQueued stands in for a scheduler that accepted the job.
func scheduleQueued() error { return nil }

// saturatedScheduler returns a scheduler (queue depth 1) whose only slot is held
// by a blocked job, plus a drain func that unblocks it and waits for it to exit.
func saturatedScheduler(t *testing.T) (*BackgroundAcquisitionScheduler, func()) {
	t.Helper()
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1), WithQueueDepth(1))

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); err != nil {
		t.Fatalf("filling Schedule = %v, want nil", err)
	}
	<-repo.started
	return scheduler, func() {
		close(repo.release)
		wg.Wait()
	}
}

func TestBackgroundScheduler_QueueFullIsObservable(t *testing.T) {
	scheduler, drain := saturatedScheduler(t)
	userId := shared.NewUserId(uuid.New())

	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); !errors.Is(err, ErrAcquisitionQueueFull) {
		t.Errorf("Schedule on full queue = %v, want ErrAcquisitionQueueFull", err)
	}
	if err := scheduler.ScheduleReplace(context.Background(), userId, domain.NewTrackId()); !errors.Is(err, ErrAcquisitionQueueFull) {
		t.Errorf("ScheduleReplace on full queue = %v, want ErrAcquisitionQueueFull", err)
	}
	drain()

	if err := scheduler.Schedule(context.Background(), userId, domain.NewTrackId(), ""); err != nil {
		t.Errorf("Schedule after drain = %v, want nil (admitted)", err)
	}
}

func TestBackgroundScheduler_InflightDedupIsNotARefusal(t *testing.T) {
	repo := &blockingRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))
	userId, trackId := shared.NewUserId(uuid.New()), domain.NewTrackId()

	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err != nil {
		t.Fatalf("first Schedule = %v, want nil", err)
	}
	<-repo.started
	if err := scheduler.Schedule(context.Background(), userId, trackId, ""); err != nil {
		t.Errorf("duplicate Schedule while in flight = %v, want nil (a job is already running)", err)
	}
	close(repo.release)
	wg.Wait()
}

func TestBackgroundScheduler_AfterShutdownRefuses(t *testing.T) {
	svc := NewAcquireTrackAudioService(&countingRepo{}, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())
	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))
	scheduler.Shutdown(context.Background())

	if err := scheduler.Schedule(context.Background(), shared.NewUserId(uuid.New()), domain.NewTrackId(), ""); !errors.Is(err, ErrSchedulerShutdown) {
		t.Errorf("Schedule after shutdown = %v, want ErrSchedulerShutdown", err)
	}
}

// TestRetryAdmission_QueueFullDoesNotBurnCooldown drives the real admission
// channel full: the rejection reaches the caller, the retry cooldown stays
// unburned, and the still-failed track is retryable as soon as the queue drains.
func TestRetryAdmission_QueueFullDoesNotBurnCooldown(t *testing.T) {
	scheduler, drain := saturatedScheduler(t)
	admission := NewRetryAdmission(newFakeCooldownStore())
	track := failedTrack(t)
	schedule := func() error { return scheduler.Schedule(context.Background(), track.UserId, track.ID, "") }

	if err := admission.Admit(context.Background(), track, schedule); !errors.Is(err, ErrAcquisitionQueueFull) {
		t.Fatalf("Admit on full queue = %v, want ErrAcquisitionQueueFull", err)
	}
	if track.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("status = %v, want still failed (retryable)", track.AcquisitionStatus)
	}
	drain()

	if err := admission.Admit(context.Background(), track, schedule); err != nil {
		t.Errorf("Admit after drain = %v, want nil (cooldown must not be burned by the shed job)", err)
	}
}

func TestReacquireAdmission_QueueFullDoesNotBurnCooldown(t *testing.T) {
	scheduler, drain := saturatedScheduler(t)
	admission := NewReacquireAdmission(newFakeCooldownStore())
	track := readyTrack(t)
	schedule := func() error { return scheduler.ScheduleReplace(context.Background(), track.UserId, track.ID) }

	if err := admission.Admit(context.Background(), track, schedule); !errors.Is(err, ErrAcquisitionQueueFull) {
		t.Fatalf("Admit on full queue = %v, want ErrAcquisitionQueueFull", err)
	}
	drain()

	if err := admission.Admit(context.Background(), track, schedule); err != nil {
		t.Errorf("Admit after drain = %v, want nil (cooldown must not be burned by the shed job)", err)
	}
}

func TestAdmission_ScheduleSkippedWhenNotAdmitted(t *testing.T) {
	called := false
	schedule := func() error { called = true; return nil }

	if err := NewRetryAdmission(newFakeCooldownStore()).Admit(context.Background(), readyTrack(t), schedule); !errors.Is(err, ErrRetryNotFailed) {
		t.Fatalf("Admit = %v, want ErrRetryNotFailed", err)
	}
	a := NewRetryAdmission(newFakeCooldownStore())
	track := failedTrack(t)
	if err := a.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	if err := a.Admit(context.Background(), track, schedule); !errors.Is(err, ErrCooldownActive) {
		t.Fatalf("second Admit = %v, want ErrCooldownActive", err)
	}
	if called {
		t.Error("schedule ran for a request admission refused")
	}
}
