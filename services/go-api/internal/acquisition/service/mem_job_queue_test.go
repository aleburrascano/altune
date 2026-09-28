package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMemJobQueue_ClaimOnEmptyQueueReturnsErrNoJobAvailable(t *testing.T) {
	q := newMemJobQueue(nil)

	if _, err := q.Claim(context.Background(), time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
		t.Fatalf("Claim on empty queue = %v, want ErrNoJobAvailable", err)
	}
}

func TestMemJobQueue_EnqueueThenClaimReturnsTheJobAndLeasesIt(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	userID := shared.NewUserId(uuid.New())
	q.rememberUser(trackID, userID)

	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, time.Now()); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}

	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}
	if job.TrackID != trackID || job.UserID != userID || job.Kind != ports.JobKindAcquire {
		t.Errorf("Claim = %+v, want the enqueued track/user/kind", job)
	}

	if _, err := q.Claim(context.Background(), time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
		t.Fatalf("second Claim while leased = %v, want ErrNoJobAvailable", err)
	}
}

func TestMemJobQueue_HeartbeatWrongFenceReturnsErrLeaseLost(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, time.Now()); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Heartbeat(context.Background(), trackID, job.Attempts+1, time.Minute); !errors.Is(err, ports.ErrLeaseLost) {
		t.Fatalf("Heartbeat with a stale fence = %v, want ErrLeaseLost", err)
	}
	if err := q.Heartbeat(context.Background(), trackID, job.Attempts, time.Minute); err != nil {
		t.Errorf("Heartbeat with the owning fence = %v, want nil", err)
	}
}

func TestMemJobQueue_ReleaseMakesTheJobClaimableAgain(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, time.Now()); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Release(context.Background(), trackID, job.Attempts, time.Now()); err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}
	if _, err := q.Claim(context.Background(), time.Minute); err != nil {
		t.Fatalf("Claim after Release = %v, want the job to be claimable again", err)
	}
}

func TestMemJobQueue_SettleRemovesTheJob(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, time.Now()); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Settle(context.Background(), trackID, job.Attempts); err != nil {
		t.Fatalf("Settle = %v, want nil", err)
	}
	if _, ok := q.jobs[trackID]; ok {
		t.Error("job still present after Settle, want it removed")
	}
}

func TestMemJobQueue_EnqueueOnLiveLeaseOfDifferentKindConflicts(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, time.Now()); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := q.Claim(context.Background(), time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Enqueue(context.Background(), trackID, ports.JobKindReplace, time.Now()); !errors.Is(err, ports.ErrJobKindConflict) {
		t.Errorf("Enqueue replace over a running acquire = %v, want ErrJobKindConflict", err)
	}
}

func TestMemJobQueue_SettleAfterAReEnqueueLeavesThePendingJobClaimable(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, time.Now()); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, time.Now()); err != nil {
		t.Fatalf("re-Enqueue while leased = %v, want nil (no-op)", err)
	}
	if err := q.Settle(context.Background(), trackID, job.Attempts); err != nil {
		t.Fatalf("Settle = %v, want nil", err)
	}
	if _, err := q.Claim(context.Background(), time.Minute); err != nil {
		t.Fatalf("Claim after Settle of a re-enqueued job = %v, want the job still claimable", err)
	}
}
