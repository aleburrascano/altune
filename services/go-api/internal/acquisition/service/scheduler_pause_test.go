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

// TestBackgroundScheduler_RuntimeKillSwitchTogglesAdmission reproduces the
// missing-kill-switch defect: a misbehaving downloader can only be stopped by
// taking the process down, because admission is gated solely on the
// process-lifetime shutdown flag. A runtime pause must refuse new jobs without
// a restart, and a resume must re-admit them — all on the same live scheduler
// instance.
//
// Setup: jobs complete immediately (release closed), so nothing stays in
// flight to confound the admission result. Enabled -> admit, Pause -> refuse
// with ErrAcquisitionPaused, Resume -> admit again. Before the fix there is no
// Pause/Resume control at all and the toggle cannot even be expressed.
func TestBackgroundScheduler_RuntimeKillSwitchTogglesAdmission(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	close(repo.release) // jobs drain immediately; admission is the only variable
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))

	user := shared.NewUserId(uuid.New())

	// Enabled by default: a job admits.
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule while enabled = %v, want nil", err)
	}
	if scheduler.Status().Paused {
		t.Fatal("Status().Paused = true before any Pause, want false")
	}
	wg.Wait()

	// Kill switch flipped off at runtime — no process restart. New jobs refused.
	scheduler.Pause()
	if !scheduler.Status().Paused {
		t.Fatal("Status().Paused = false after Pause, want true")
	}
	err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), "")
	if !errors.Is(err, ErrAcquisitionPaused) {
		t.Fatalf("schedule while paused = %v, want ErrAcquisitionPaused", err)
	}
	if err := scheduler.ScheduleReplace(context.Background(), user, domain.NewTrackId()); !errors.Is(err, ErrAcquisitionPaused) {
		t.Fatalf("replace while paused = %v, want ErrAcquisitionPaused", err)
	}

	// Resumed at runtime: admission returns without a restart.
	scheduler.Resume()
	if scheduler.Status().Paused {
		t.Fatal("Status().Paused = true after Resume, want false")
	}
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule after resume = %v, want nil (kill switch must be reversible)", err)
	}
	wg.Wait()
}

// TestBackgroundScheduler_PauseLeavesInflightRunning pins that pausing is a
// kill switch on *admission* only: a job already in flight when the pause lands
// runs to completion, and no admission or principal reservation is leaked (a
// later resume admits the configured depth again). A pause that stranded the
// in-flight slot would deadlock the queue.
func TestBackgroundScheduler_PauseLeavesInflightRunning(t *testing.T) {
	repo := &burstRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem, WithQueueDepth(1))

	user := shared.NewUserId(uuid.New())
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule = %v, want nil", err)
	}
	<-repo.started // one job in flight, holding the sole admission + worker slot

	// Pause after the job started: it must keep running (below, closing release
	// lets it finish). New arrivals are refused.
	scheduler.Pause()
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); !errors.Is(err, ErrAcquisitionPaused) {
		t.Fatalf("schedule while paused = %v, want ErrAcquisitionPaused", err)
	}

	// Let the in-flight job drain: it must complete despite the pause, refunding
	// its slot rather than leaking it.
	close(repo.release)
	wg.Wait()

	// Resume: the refunded slot is available again, proving nothing leaked.
	scheduler.Resume()
	if err := scheduler.Schedule(context.Background(), user, domain.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule after drain+resume = %v, want nil (slot must be refunded)", err)
	}
	wg.Wait()
}
