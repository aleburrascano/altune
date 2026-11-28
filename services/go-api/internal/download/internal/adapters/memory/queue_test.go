package memory_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

const lease = time.Minute

func newQueue() (*memory.Queue, *clocktest.Manual) {
	c := clocktest.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return memory.New(nil, c.Now), c
}

func enqueue(t *testing.T, q *memory.Queue, trackID ids.TrackId, kind ports.JobKind, delay time.Duration) {
	t.Helper()
	if err := q.Enqueue(context.Background(), trackID, kind, delay); err != nil {
		t.Fatalf("Enqueue %s: %v", kind, err)
	}
}

func claim(t *testing.T, q *memory.Queue) ports.Job {
	t.Helper()
	job, err := q.Claim(context.Background(), lease)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	return job
}

func claimUpgrade(t *testing.T, q *memory.Queue) ports.Job {
	t.Helper()
	job, err := q.ClaimUpgrade(context.Background(), lease)
	if err != nil {
		t.Fatalf("ClaimUpgrade: %v", err)
	}
	return job
}

func wantNothingToClaim(t *testing.T, q *memory.Queue, when string) {
	t.Helper()
	if job, err := q.Claim(context.Background(), lease); !errors.Is(err, domain.ErrNoJobAvailable) {
		t.Fatalf("Claim %s = %+v, %v, want ErrNoJobAvailable", when, job, err)
	}
}

func wantNoUpgradeToClaim(t *testing.T, q *memory.Queue, when string) {
	t.Helper()
	if job, err := q.ClaimUpgrade(context.Background(), lease); !errors.Is(err, domain.ErrNoJobAvailable) {
		t.Fatalf("ClaimUpgrade %s = %+v, %v, want ErrNoJobAvailable", when, job, err)
	}
}

func wantDepth(t *testing.T, q *memory.Queue, wantPending int, wantOldest time.Duration) {
	t.Helper()
	pending, oldest, err := q.PendingDepth(context.Background())
	if err != nil || pending != wantPending || oldest != wantOldest {
		t.Fatalf("PendingDepth = %d, %v, %v, want %d, %v, nil", pending, oldest, err, wantPending, wantOldest)
	}
}

func woken(wake <-chan struct{}) bool {
	select {
	case <-wake:
		return true
	default:
		return false
	}
}

func TestQueue_AnExpiringLeaseIsReclaimedOnlyOnceItHasLapsed(t *testing.T) {
	q, c := newQueue()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	first := claim(t, q)

	c.Advance(lease - time.Nanosecond)
	wantNothingToClaim(t, q, "a moment before the lease lapses")

	c.Advance(time.Nanosecond)
	second := claim(t, q)
	if second.TrackID != trackID || second.ClaimNumber == first.ClaimNumber || second.Run != 2 {
		t.Errorf("reclaim = %+v, want the same track at run 2 under a fence other than %v", second, first.ClaimNumber)
	}
}

func TestQueue_AReEnqueueAfterPriorClaimsStartsANewRunAndAdvancesTheFence(t *testing.T) {
	q, c := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	var stale ports.Job
	for range 3 {
		stale = claim(t, q)
		c.Advance(2 * lease)
	}

	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	job := claim(t, q)

	if job.Run != 1 || job.ClaimNumber != 5 {
		t.Errorf("Run/Fence = %d/%d, want 1/5", job.Run, job.ClaimNumber)
	}
	if err := q.Settle(ctx, trackID, stale.ClaimNumber); !errors.Is(err, domain.ErrLeaseLost) {
		t.Errorf("stale Settle = %v, want ErrLeaseLost", err)
	}
}

func TestQueue_AJobEnqueuedWithADelayIsClaimableOnceTheDelayHasPassed(t *testing.T) {
	q, c := newQueue()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindAcquire, 10*time.Second)

	c.Advance(10*time.Second - time.Nanosecond)
	wantNothingToClaim(t, q, "a moment before the delay passed")

	c.Advance(time.Nanosecond)
	if job := claim(t, q); job.TrackID != trackID || job.Waited != 0 {
		t.Errorf("Claim = %+v, want track %s having waited 0 past its delay", job, trackID)
	}
}

func TestQueue_ReleaseWithADelayIsClaimableOnlyOnceTheClockPassesIt(t *testing.T) {
	q, c := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	job := claim(t, q)
	if err := q.Release(ctx, trackID, job.ClaimNumber, 10*time.Second); err != nil {
		t.Fatalf("Release = %v", err)
	}

	c.Advance(9 * time.Second)
	wantNothingToClaim(t, q, "before the delay elapsed")

	c.Advance(time.Second)
	if again := claim(t, q); again.TrackID != trackID {
		t.Errorf("Claim after the delay = %+v, want track %s", again, trackID)
	}
}

func TestQueue_ReleaseOfAnUnknownTrackIsAnErrorButNotALostLease(t *testing.T) {
	q, _ := newQueue()

	err := q.Release(context.Background(), ids.NewTrackId(), 1, 0)

	if err == nil || errors.Is(err, domain.ErrLeaseLost) {
		t.Errorf("Release of a track never enqueued = %v, want a not-found error", err)
	}
}

func TestQueue_ReleaseWithAStaleFenceLeavesTheOwnersLeaseLive(t *testing.T) {
	q, _ := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	owner := claim(t, q)

	if err := q.Release(ctx, trackID, owner.ClaimNumber+1, 0); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("Release with a stale fence = %v, want ErrLeaseLost", err)
	}

	wantNothingToClaim(t, q, "after a stale Release")
	if err := q.Heartbeat(ctx, trackID, owner.ClaimNumber, lease); err != nil {
		t.Errorf("owner Heartbeat = %v, want nil", err)
	}
}

func TestQueue_SettleRemovesTheJob(t *testing.T) {
	q, c := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	job := claim(t, q)

	if err := q.Settle(ctx, trackID, job.ClaimNumber); err != nil {
		t.Fatalf("Settle = %v, want nil", err)
	}

	c.Advance(2 * lease)
	wantNothingToClaim(t, q, "after Settle")
	wantDepth(t, q, 0, 0)
	if err := q.Release(ctx, trackID, job.ClaimNumber, 0); err == nil || errors.Is(err, domain.ErrLeaseLost) {
		t.Errorf("Release after Settle = %v, want the not-found error of a removed job", err)
	}
	if err := q.Settle(ctx, trackID, job.ClaimNumber); err != nil {
		t.Errorf("a second Settle = %v, want nil: settling a removed job is a no-op", err)
	}
}

func TestQueue_ClaimReportsHowLongTheJobWaited(t *testing.T) {
	q, c := newQueue()
	enqueue(t, q, ids.NewTrackId(), ports.JobKindAcquire, 0)

	c.Advance(45 * time.Second)

	if job := claim(t, q); job.Waited != 45*time.Second {
		t.Errorf("Waited = %v, want 45s", job.Waited)
	}
}

func TestQueue_ClaimTakesTheEarliestJobAmongEquallyBusyUsers(t *testing.T) {
	q, c := newQueue()
	user := ids.NewUserId(uuid.New())
	tracks := make([]ids.TrackId, 4)
	for i := range tracks {
		tracks[i] = ids.NewTrackId()
		q.RememberUser(tracks[i], user)
	}
	for _, i := range []int{2, 0, 3, 1} {
		enqueue(t, q, tracks[i], ports.JobKindAcquire, time.Duration(i)*time.Second)
	}
	c.Advance(time.Minute)

	for i, want := range tracks {
		if got := claim(t, q); got.TrackID != want {
			t.Fatalf("Claim #%d = %s, want the job available earliest, %s", i+1, got.TrackID, want)
		}
	}
}

func TestQueue_ClaimPrefersTheUserWithFewerLiveLeases(t *testing.T) {
	q, c := newQueue()
	busy, lessBusy := ids.NewUserId(uuid.New()), ids.NewUserId(uuid.New())
	add := func(user ids.UserId) ids.TrackId {
		trackID := ids.NewTrackId()
		q.RememberUser(trackID, user)
		enqueue(t, q, trackID, ports.JobKindAcquire, 0)
		c.Advance(time.Second)
		return trackID
	}
	add(busy)
	add(busy)
	add(lessBusy)
	busyWaiting := add(busy)
	lessBusyWaiting := add(lessBusy)
	for range 3 {
		claim(t, q)
	}

	if got := claim(t, q); got.TrackID != lessBusyWaiting {
		t.Errorf("Claim = %s, want %s of the user with one live lease ahead of %s of the user with two", got.TrackID, lessBusyWaiting, busyWaiting)
	}
}

func TestQueue_RememberUserNamesTheOwnerOfAJobAlreadyQueued(t *testing.T) {
	q, _ := newQueue()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	user := ids.NewUserId(uuid.New())

	q.RememberUser(trackID, user)

	if job := claim(t, q); job.UserID != user {
		t.Errorf("Claim UserID = %s, want %s remembered after the enqueue", job.UserID, user)
	}
}

func TestQueue_EnqueueWakesTheWorkersWithoutBlockingOnAFullWakeChannel(t *testing.T) {
	wake := make(chan struct{}, 1)
	q := memory.New(wake, time.Now)
	ctx := context.Background()
	trackID := ids.NewTrackId()

	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	enqueue(t, q, ids.NewTrackId(), ports.JobKindAcquire, 0)
	if !woken(wake) {
		t.Fatal("no wake after Enqueue")
	}

	job := claim(t, q)
	if woken(wake) {
		t.Fatal("Claim woke the workers, want only enqueues to wake them")
	}
	if err := q.Enqueue(ctx, job.TrackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue over the live lease: %v", err)
	}
	if !woken(wake) {
		t.Error("no wake after an Enqueue over a live lease")
	}
}

func TestQueue_AnEnqueueOverALiveLeaseRestartsTheRunCountForTheNextClaim(t *testing.T) {
	q, c := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	claim(t, q)
	c.Advance(2 * lease)
	owner := claim(t, q)

	enqueue(t, q, trackID, ports.JobKindAcquire, 5*time.Second)
	if err := q.Settle(ctx, trackID, owner.ClaimNumber); err != nil {
		t.Fatalf("owner Settle = %v", err)
	}

	c.Advance(5*time.Second - time.Nanosecond)
	wantNothingToClaim(t, q, "before the re-enqueue's delay passed")
	c.Advance(time.Nanosecond)
	if next := claim(t, q); next.Run != 1 {
		t.Errorf("Run after a re-enqueue over a live lease = %d, want a new run at 1", next.Run)
	}
}

func TestQueue_AReleaseClearsAReEnqueueSoTheNextSettleRemovesTheJob(t *testing.T) {
	q, c := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	owner := claim(t, q)
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)

	if err := q.Release(ctx, trackID, owner.ClaimNumber, 0); err != nil {
		t.Fatalf("Release = %v", err)
	}
	next := claim(t, q)
	if err := q.Settle(ctx, trackID, next.ClaimNumber); err != nil {
		t.Fatalf("Settle = %v", err)
	}

	c.Advance(2 * lease)
	wantNothingToClaim(t, q, "after the Settle of the released job")
}

func TestQueue_UserAddsKeepPriorityOverUpgrades(t *testing.T) {
	q, _ := newQueue()
	ctx := context.Background()
	upgrading, added := ids.NewTrackId(), ids.NewTrackId()
	enqueue(t, q, upgrading, ports.JobKindUpgrade, 0)
	enqueue(t, q, added, ports.JobKindAcquire, 0)

	wantNoUpgradeToClaim(t, q, "while a user add waits")
	userJob := claim(t, q)
	if userJob.TrackID != added {
		t.Fatalf("Claim = %+v, want the user's add", userJob)
	}
	wantNothingToClaim(t, q, "with only an upgrade queued: an upgrade is not a user job")
	if err := q.Settle(ctx, userJob.TrackID, userJob.ClaimNumber); err != nil {
		t.Fatalf("Settle = %v", err)
	}

	upgrade := claimUpgrade(t, q)
	if upgrade.TrackID != upgrading || upgrade.Kind != ports.JobKindUpgrade {
		t.Fatalf("ClaimUpgrade = %+v, want the upgrade once no user add waits", upgrade)
	}
}

func TestQueue_ClaimUpgradeSkipsUpgradesThatAreDelayedOrLeased(t *testing.T) {
	q, c := newQueue()
	delayed, running := ids.NewTrackId(), ids.NewTrackId()
	enqueue(t, q, running, ports.JobKindUpgrade, 0)
	claimUpgrade(t, q)
	enqueue(t, q, delayed, ports.JobKindUpgrade, time.Second)

	wantNoUpgradeToClaim(t, q, "with one upgrade leased and one delayed")

	c.Advance(time.Second)
	if got := claimUpgrade(t, q); got.TrackID != delayed {
		t.Errorf("ClaimUpgrade = %s, want the delayed %s once it is due", got.TrackID, delayed)
	}
}

func TestQueue_AUserJobThatIsNotDueYetDoesNotHoldBackUpgrades(t *testing.T) {
	q, _ := newQueue()
	upgrading := ids.NewTrackId()
	enqueue(t, q, ids.NewTrackId(), ports.JobKindAcquire, time.Hour)
	enqueue(t, q, upgrading, ports.JobKindUpgrade, 0)

	if got := claimUpgrade(t, q); got.TrackID != upgrading {
		t.Errorf("ClaimUpgrade = %s, want %s while the only user job is delayed", got.TrackID, upgrading)
	}
}

func TestQueue_PendingDepthCountsWaitingUserJobsAndAgesTheOldestDueOne(t *testing.T) {
	q, c := newQueue()
	wantDepth(t, q, 0, 0)

	enqueue(t, q, ids.NewTrackId(), ports.JobKindAcquire, time.Hour)
	wantDepth(t, q, 1, 0)

	enqueue(t, q, ids.NewTrackId(), ports.JobKindAcquire, 0)
	c.Advance(10 * time.Second)
	enqueue(t, q, ids.NewTrackId(), ports.JobKindAcquire, 0)
	c.Advance(5 * time.Second)
	enqueue(t, q, ids.NewTrackId(), ports.JobKindAcquire, -20*time.Second)
	wantDepth(t, q, 4, 20*time.Second)

	enqueue(t, q, ids.NewTrackId(), ports.JobKindAcquire, -time.Second)
	wantDepth(t, q, 5, 20*time.Second)
}

func TestQueue_PendingDepthLeavesOutALiveLeaseButCountsALapsedOne(t *testing.T) {
	q, c := newQueue()
	enqueue(t, q, ids.NewTrackId(), ports.JobKindAcquire, 0)
	claim(t, q)
	wantDepth(t, q, 0, 0)

	c.Advance(lease)

	wantDepth(t, q, 1, lease)
}

func TestQueue_UpgradesAreNotCountedInTheUserQueueDepth(t *testing.T) {
	q, _ := newQueue()
	enqueue(t, q, ids.NewTrackId(), ports.JobKindUpgrade, 0)

	wantDepth(t, q, 0, 0)
}

func TestQueue_AnUpgradeNeverOverwritesAQueuedUserJob(t *testing.T) {
	q, _ := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindReplace, 0)

	if err := q.Enqueue(ctx, trackID, ports.JobKindUpgrade, 0); !errors.Is(err, domain.ErrJobKindConflict) {
		t.Fatalf("Enqueue upgrade over a queued replace = %v, want ErrJobKindConflict", err)
	}

	if job := claim(t, q); job.Kind != ports.JobKindReplace {
		t.Errorf("Claim = %+v, want the user's replace still queued", job)
	}
	enqueue(t, q, trackID, ports.JobKindReplace, 0)
}

func TestQueue_AnUpgradeOverAQueuedUpgradeIsAccepted(t *testing.T) {
	q, _ := newQueue()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindUpgrade, 0)

	enqueue(t, q, trackID, ports.JobKindUpgrade, 0)

	if got := claimUpgrade(t, q); got.TrackID != trackID {
		t.Errorf("ClaimUpgrade = %+v, want %s", got, trackID)
	}
}

func TestQueue_AnUpgradeOverARunningUpgradeKeepsTheLease(t *testing.T) {
	q, _ := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindUpgrade, 0)
	running := claimUpgrade(t, q)

	enqueue(t, q, trackID, ports.JobKindUpgrade, 0)

	if err := q.VerifyLease(ctx, trackID, running.ClaimNumber); err != nil {
		t.Errorf("VerifyLease after a second upgrade enqueue = %v, want the running upgrade's lease kept", err)
	}
}

func TestQueue_AUserJobTakesOverARunningUpgrade(t *testing.T) {
	q, _ := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	enqueue(t, q, trackID, ports.JobKindUpgrade, 0)
	upgrade := claimUpgrade(t, q)

	if err := q.Enqueue(ctx, trackID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace over a running upgrade = %v, want the user's job to win", err)
	}

	if err := q.Heartbeat(ctx, trackID, upgrade.ClaimNumber, lease); !errors.Is(err, domain.ErrLeaseLost) {
		t.Errorf("upgrade Heartbeat = %v, want ErrLeaseLost so the upgrade stops", err)
	}
	if job := claim(t, q); job.Kind != ports.JobKindReplace || job.Run != 1 {
		t.Errorf("Claim = %+v, want the user's replace at run 1", job)
	}
}

func TestQueue_VerifyLeaseHoldsOnlyForTheCurrentFenceOfALiveLease(t *testing.T) {
	q, c := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	if err := q.VerifyLease(ctx, trackID, 1); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("VerifyLease before any enqueue = %v, want ErrLeaseLost", err)
	}
	enqueue(t, q, trackID, ports.JobKindUpgrade, 0)
	if err := q.VerifyLease(ctx, trackID, 1); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("VerifyLease before the claim = %v, want ErrLeaseLost", err)
	}
	job := claimUpgrade(t, q)

	if err := q.VerifyLease(ctx, trackID, job.ClaimNumber); err != nil {
		t.Fatalf("VerifyLease on the claimed fence = %v, want nil", err)
	}
	if err := q.VerifyLease(ctx, trackID, job.ClaimNumber+1); !errors.Is(err, domain.ErrLeaseLost) {
		t.Errorf("VerifyLease on another fence = %v, want ErrLeaseLost", err)
	}

	c.Advance(lease)
	if err := q.VerifyLease(ctx, trackID, job.ClaimNumber); !errors.Is(err, domain.ErrLeaseLost) {
		t.Errorf("VerifyLease once the lease lapsed = %v, want ErrLeaseLost", err)
	}
}

func TestQueue_HeartbeatExtendsTheLeaseFromNow(t *testing.T) {
	q, c := newQueue()
	ctx := context.Background()
	trackID := ids.NewTrackId()
	if err := q.Heartbeat(ctx, trackID, 1, lease); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("Heartbeat of a track never enqueued = %v, want ErrLeaseLost", err)
	}
	enqueue(t, q, trackID, ports.JobKindAcquire, 0)
	if err := q.Heartbeat(ctx, trackID, 0, lease); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("Heartbeat of a job not yet claimed = %v, want ErrLeaseLost", err)
	}
	job := claim(t, q)

	c.Advance(30 * time.Second)
	if err := q.Heartbeat(ctx, trackID, job.ClaimNumber, lease); err != nil {
		t.Fatalf("Heartbeat = %v", err)
	}

	c.Advance(lease - time.Nanosecond)
	wantNothingToClaim(t, q, "a moment before the extended lease lapses")
	c.Advance(time.Nanosecond)
	claim(t, q)
}

type tickingClock struct{ now time.Time }

func (c *tickingClock) Now() time.Time {
	c.now = c.now.Add(time.Millisecond)
	return c.now
}

type contractQueue struct {
	*memory.Queue
	clock *tickingClock
}

func (q contractQueue) AddTrack(_ *testing.T, userID ids.UserId) ids.TrackId {
	trackID := ids.NewTrackId()
	q.RememberUser(trackID, userID)
	return trackID
}

func (q contractQueue) ExpireLease(*testing.T, ids.TrackId) {
	q.clock.now = q.clock.now.Add(2 * time.Hour)
}

func TestQueue_Contract(t *testing.T) {
	contracttest.JobQueueContract(t, func(*testing.T) contracttest.JobQueueUnderTest {
		c := &tickingClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		return contractQueue{Queue: memory.New(nil, c.Now), clock: c}
	})
}
