package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"testing"
	"time"
)

// TestStalePendingRecovery is the regression for the stuck-at-pending bug: a track
// scheduled for acquisition whose in-memory job is lost to a dead process must not
// stay pending (and therefore unretryable) forever. The durable in-flight marker
// plus a reconcile sweep moves it to failed, the state the retry path admits.
func TestStalePendingRecovery(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()

	// A scheduler that records the request but never runs the job, standing in for
	// a process that accepted the schedule and then died before RunPipeline finished.
	lostJobs := &catalogtest.Scheduler{}
	addSvc := NewAddTrackService(repo, WithAcquisitionScheduler(lostJobs))

	out, err := addSvc.Execute(ctx, userId, AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("AddTrack: %v", err)
	}
	track := out.Track

	if len(lostJobs.TrackIds) != 1 || lostJobs.TrackIds[0] != track.ID {
		t.Fatalf("scheduler did not receive the track: %+v", lostJobs.TrackIds)
	}
	if track.AcquisitionStatus != domain.AcquisitionPending {
		t.Fatalf("status = %v, want pending", track.AcquisitionStatus)
	}
	if track.AcquisitionStartedAt == nil {
		t.Fatal("no durable in-flight marker persisted for the pending track")
	}

	// The track is still queryable after the "restart".
	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil || got == nil {
		t.Fatalf("GetByID after restart: track=%v err=%v", got, err)
	}

	reconcile := NewReconcileStalePendingService(repo)

	// A freshly scheduled track is NOT stale: the sweep must leave it alone so a
	// legitimately in-progress acquisition on a live process is never killed.
	recovered, err := reconcile.Execute(ctx)
	if err != nil {
		t.Fatalf("reconcile (fresh): %v", err)
	}
	if recovered != 0 {
		t.Fatalf("recovered %d fresh tracks, want 0", recovered)
	}
	if track.AcquisitionStatus != domain.AcquisitionPending {
		t.Fatalf("fresh track status = %v, want still pending", track.AcquisitionStatus)
	}

	// Simulate the grace window elapsing: the job has been "in flight" far too long.
	stale := time.Now().UTC().Add(-2 * DefaultStalePendingGrace)
	track.AcquisitionStartedAt = &stale

	recovered, err = reconcile.Execute(ctx)
	if err != nil {
		t.Fatalf("reconcile (stale): %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered %d stale tracks, want 1", recovered)
	}

	healed, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil || healed == nil {
		t.Fatalf("GetByID after reconcile: track=%v err=%v", healed, err)
	}
	// Failed is exactly the precondition the retry endpoint requires
	// (acquisition/service/retry_admission.go): the track is now recoverable.
	if healed.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("status after reconcile = %v, want failed", healed.AcquisitionStatus)
	}
	if healed.FailureReason == nil || *healed.FailureReason != domain.ReasonAcquisitionInterrupted {
		t.Fatalf("failure reason = %v, want %q", healed.FailureReason, domain.ReasonAcquisitionInterrupted)
	}
	if healed.AcquisitionStartedAt != nil {
		t.Errorf("in-flight marker = %v, want cleared after recovery", healed.AcquisitionStartedAt)
	}
}

func TestReconcileStalePending_RepoErrorPropagates(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	repo.ErrOnFailStale = errors.New("db down")
	svc := NewReconcileStalePendingService(repo)

	if _, err := svc.Execute(ctx); err == nil {
		t.Fatal("expected error to propagate, got nil")
	}
}

func TestReconcileStalePending_GraceOverride(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()

	track := seedTrack(t, repo, userId, "Track", "Artist", "Album")
	startedAt := time.Now().UTC().Add(-2 * time.Minute)
	track.AcquisitionStartedAt = &startedAt

	// Default grace (15m) leaves a 2-minute-old track alone.
	if n, err := NewReconcileStalePendingService(repo).Execute(ctx); err != nil || n != 0 {
		t.Fatalf("default grace recovered=%d err=%v, want 0", n, err)
	}

	// A 1-minute grace makes the same track stale.
	svc := NewReconcileStalePendingService(repo, WithStalePendingGrace(time.Minute))
	if n, err := svc.Execute(ctx); err != nil || n != 1 {
		t.Fatalf("1m grace recovered=%d err=%v, want 1", n, err)
	}
}
