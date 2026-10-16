package service

import (
	"altune/go-api/internal/acquisition/acquisitiontest"
	acqdomain "altune/go-api/internal/acquisition/domain"
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

	if _, err := q.Claim(context.Background(), time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Fatalf("Claim on empty queue = %v, want ErrNoJobAvailable", err)
	}
}

func TestMemJobQueue_EnqueueThenClaimReturnsTheJobAndLeasesIt(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	userID := shared.NewUserId(uuid.New())
	q.rememberUser(trackID, userID)

	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}

	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}
	if job.TrackID != trackID || job.UserID != userID || job.Kind != ports.JobKindAcquire {
		t.Errorf("Claim = %+v, want the enqueued track/user/kind", job)
	}

	if _, err := q.Claim(context.Background(), time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Fatalf("second Claim while leased = %v, want ErrNoJobAvailable", err)
	}
}

func TestMemJobQueue_HeartbeatWrongFenceReturnsErrLeaseLost(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Heartbeat(context.Background(), trackID, job.Fence+1, time.Minute); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("Heartbeat with a stale fence = %v, want ErrLeaseLost", err)
	}
	if err := q.Heartbeat(context.Background(), trackID, job.Fence, time.Minute); err != nil {
		t.Errorf("Heartbeat with the owning fence = %v, want nil", err)
	}
}

func TestMemJobQueue_ReleaseMakesTheJobClaimableAgain(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Release(context.Background(), trackID, job.Fence, 0); err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}
	if _, err := q.Claim(context.Background(), time.Minute); err != nil {
		t.Fatalf("Claim after Release = %v, want the job to be claimable again", err)
	}
}

func TestMemJobQueue_SettleRemovesTheJob(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Settle(context.Background(), trackID, job.Fence); err != nil {
		t.Fatalf("Settle = %v, want nil", err)
	}
	if _, ok := q.jobs[trackID]; ok {
		t.Error("job still present after Settle, want it removed")
	}
}

func TestMemJobQueue_EnqueueOnLiveLeaseOfDifferentKindConflicts(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := q.Claim(context.Background(), time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Enqueue(context.Background(), trackID, ports.JobKindReplace, 0); !errors.Is(err, acqdomain.ErrJobKindConflict) {
		t.Errorf("Enqueue replace over a running acquire = %v, want ErrJobKindConflict", err)
	}
}

func TestMemJobQueue_SettleAfterAReEnqueueLeavesThePendingJobClaimable(t *testing.T) {
	q := newMemJobQueue(nil)
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("re-Enqueue while leased = %v, want nil (no-op)", err)
	}
	if err := q.Settle(context.Background(), trackID, job.Fence); err != nil {
		t.Fatalf("Settle = %v, want nil", err)
	}
	if _, err := q.Claim(context.Background(), time.Minute); err != nil {
		t.Fatalf("Claim after Settle of a re-enqueued job = %v, want the job still claimable", err)
	}
}

func TestMemJobQueue_ClaimReclaimsAJobWhoseLeaseExpired(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := newMemJobQueueWithClock(nil, func() time.Time { return clock })
	trackID := domain.NewTrackId()
	if err := q.Enqueue(context.Background(), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	first, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	clock = clock.Add(30 * time.Second)
	if _, err := q.Claim(context.Background(), time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Fatalf("Claim before expiry = %v, want ErrNoJobAvailable", err)
	}

	clock = clock.Add(31 * time.Second)
	second, err := q.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Claim after expiry = %v, want the job again", err)
	}
	if second.TrackID != trackID || second.Fence == first.Fence {
		t.Errorf("reclaim = %+v, want same track with a new fence (first %v)", second, first.Fence)
	}
}

func TestMemJobQueue_ReEnqueueAfterPriorClaimsStartsANewRunButKeepsTheFence(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := newMemJobQueueWithClock(nil, func() time.Time { return clock })
	ctx := context.Background()
	trackID := domain.NewTrackId()
	if err := q.Enqueue(ctx, trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	var stale ports.Job
	for i := 0; i < 3; i++ {
		job, err := q.Claim(ctx, time.Minute)
		if err != nil {
			t.Fatalf("Claim #%d = %v", i+1, err)
		}
		stale = job
		clock = clock.Add(2 * time.Minute)
	}

	if err := q.Enqueue(ctx, trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("re-Enqueue = %v", err)
	}
	job, err := q.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim after re-Enqueue = %v", err)
	}

	if job.Run != 1 || job.Attempts != 4 {
		t.Errorf("Run/Attempts = %d/%d, want 1/4", job.Run, job.Attempts)
	}
	if err := q.Settle(ctx, trackID, stale.Fence); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("stale Settle = %v, want ErrLeaseLost", err)
	}
}

func TestMemJobQueue_ReleaseWithADelayIsClaimableOnlyOnceTheQueueClockPassesIt(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := newMemJobQueueWithClock(nil, func() time.Time { return clock })
	ctx := context.Background()
	trackID := domain.NewTrackId()
	if err := q.Enqueue(ctx, trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := q.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}
	if err := q.Release(ctx, trackID, job.Fence, 10*time.Second); err != nil {
		t.Fatalf("Release = %v", err)
	}

	clock = clock.Add(9 * time.Second)
	if _, err := q.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Fatalf("Claim before the delay elapsed = %v, want ErrNoJobAvailable", err)
	}
	clock = clock.Add(time.Second)
	if _, err := q.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim after the delay elapsed = %v, want the job", err)
	}
}

func TestMemJobQueue_ClaimReportsHowLongTheJobWaited(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := newMemJobQueueWithClock(nil, func() time.Time { return clock })
	ctx := context.Background()
	if err := q.Enqueue(ctx, domain.NewTrackId(), ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}

	clock = clock.Add(45 * time.Second)
	job, err := q.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}
	if job.Waited != 45*time.Second {
		t.Errorf("Waited = %v, want 45s", job.Waited)
	}
}

func TestMemJobQueue_SettleWithAnExpiredClaimsFenceKeepsAJobEnqueuedAfterTheLeaseExpired(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := newMemJobQueueWithClock(nil, func() time.Time { return clock })
	ctx := context.Background()
	trackID := domain.NewTrackId()
	if err := q.Enqueue(ctx, trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	stale, err := q.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}
	clock = clock.Add(2 * time.Minute)
	if err := q.Enqueue(ctx, trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue after lease expiry = %v", err)
	}

	if err := q.Settle(ctx, trackID, stale.Fence); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("Settle with the expired claim's fence = %v, want ErrLeaseLost", err)
	}
	if _, err := q.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim after the stale Settle = %v, want the enqueued job", err)
	}
}

func TestMemJobQueue_UserAddsKeepPriorityOverUpgrades(t *testing.T) {
	q := newMemJobQueue(nil)
	ctx := context.Background()
	upgrading, added := domain.NewTrackId(), domain.NewTrackId()
	if err := q.Enqueue(ctx, upgrading, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v", err)
	}
	if err := q.Enqueue(ctx, added, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue add = %v", err)
	}

	if _, err := q.ClaimUpgrade(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Fatalf("ClaimUpgrade while a user add waits = %v, want ErrNoJobAvailable", err)
	}
	userJob, err := q.Claim(ctx, time.Minute)
	if err != nil || userJob.TrackID != added {
		t.Fatalf("Claim = %+v, %v, want the user's add", userJob, err)
	}
	if _, err := q.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Fatalf("second Claim = %v, want ErrNoJobAvailable: an upgrade is not a user job", err)
	}
	if err := q.Settle(ctx, userJob.TrackID, userJob.Fence); err != nil {
		t.Fatalf("Settle = %v", err)
	}

	upgrade, err := q.ClaimUpgrade(ctx, time.Minute)
	if err != nil || upgrade.TrackID != upgrading || upgrade.Kind != ports.JobKindUpgrade {
		t.Fatalf("ClaimUpgrade = %+v, %v, want the upgrade once no user add waits", upgrade, err)
	}
}

func TestMemJobQueue_UpgradesAreNotCountedInTheUserQueueDepth(t *testing.T) {
	q := newMemJobQueue(nil)
	if err := q.Enqueue(context.Background(), domain.NewTrackId(), ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v", err)
	}

	pending, _, err := q.PendingDepth(context.Background())

	if err != nil || pending != 0 {
		t.Errorf("PendingDepth = %d, %v, want 0: the depth alert is about user adds", pending, err)
	}
}

func TestMemJobQueue_AnUpgradeNeverOverwritesAQueuedUserJob(t *testing.T) {
	q := newMemJobQueue(nil)
	ctx := context.Background()
	trackID := domain.NewTrackId()
	if err := q.Enqueue(ctx, trackID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace = %v", err)
	}

	if err := q.Enqueue(ctx, trackID, ports.JobKindUpgrade, 0); !errors.Is(err, acqdomain.ErrJobKindConflict) {
		t.Fatalf("Enqueue upgrade over a queued replace = %v, want ErrJobKindConflict", err)
	}

	job, err := q.Claim(ctx, time.Minute)
	if err != nil || job.Kind != ports.JobKindReplace {
		t.Errorf("Claim = %+v, %v, want the user's replace still queued", job, err)
	}
}

func TestMemJobQueue_AUserJobTakesOverARunningUpgrade(t *testing.T) {
	q := newMemJobQueue(nil)
	ctx := context.Background()
	trackID := domain.NewTrackId()
	if err := q.Enqueue(ctx, trackID, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v", err)
	}
	upgrade, err := q.ClaimUpgrade(ctx, time.Minute)
	if err != nil {
		t.Fatalf("ClaimUpgrade = %v", err)
	}

	if err := q.Enqueue(ctx, trackID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace over a running upgrade = %v, want the user's job to win", err)
	}

	if err := q.Heartbeat(ctx, trackID, upgrade.Fence, time.Minute); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("upgrade Heartbeat = %v, want ErrLeaseLost so the upgrade stops", err)
	}
	if job, err := q.Claim(ctx, time.Minute); err != nil || job.Kind != ports.JobKindReplace {
		t.Errorf("Claim = %+v, %v, want the user's replace", job, err)
	}
}

func TestMemJobQueue_VerifyLeaseHoldsOnlyForTheCurrentFenceOfALiveLease(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := newMemJobQueueWithClock(nil, func() time.Time { return now })
	ctx := context.Background()
	trackID := shared.NewTrackId()
	if err := q.Enqueue(ctx, trackID, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade: %v", err)
	}
	job, err := q.ClaimUpgrade(ctx, time.Minute)
	if err != nil {
		t.Fatalf("ClaimUpgrade: %v", err)
	}
	if err := q.VerifyLease(ctx, trackID, job.Fence); err != nil {
		t.Fatalf("VerifyLease on the claimed fence = %v, want nil", err)
	}

	now = now.Add(2 * time.Minute)
	if err := q.VerifyLease(ctx, trackID, job.Fence); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("VerifyLease after the lease expired = %v, want ErrLeaseLost", err)
	}
}

type contractMemJobQueue struct {
	*memJobQueue
}

func (q contractMemJobQueue) AddTrack(_ *testing.T, userID shared.UserId) shared.TrackId {
	trackID := shared.NewTrackId()
	q.rememberUser(trackID, userID)
	return trackID
}

func (q contractMemJobQueue) ExpireLease(t *testing.T, trackID shared.TrackId) {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		t.Fatalf("expire lease: track %s has no job", trackID)
	}
	j.leaseUntil = q.now().Add(-time.Second)
}

func TestMemJobQueue_Contract(t *testing.T) {
	acquisitiontest.JobQueueContract(t, func(*testing.T) acquisitiontest.JobQueueUnderTest {
		return contractMemJobQueue{newMemJobQueue(nil)}
	})
}
