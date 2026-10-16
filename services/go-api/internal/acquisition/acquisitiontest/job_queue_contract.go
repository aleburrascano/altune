package acquisitiontest

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type JobQueueUnderTest interface {
	ports.JobQueue
	AddTrack(t *testing.T, userID shared.UserId) shared.TrackId
	ExpireLease(t *testing.T, trackID shared.TrackId)
}

const contractLease = time.Minute

func enqueued(t *testing.T, q JobQueueUnderTest, userID shared.UserId) shared.TrackId {
	t.Helper()
	trackID := q.AddTrack(t, userID)
	enqueue(t, q, trackID, ports.JobKindAcquire)
	return trackID
}

func enqueue(t *testing.T, q JobQueueUnderTest, trackID shared.TrackId, kind ports.JobKind) {
	t.Helper()
	if err := q.Enqueue(context.Background(), trackID, kind, 0); err != nil {
		t.Fatalf("Enqueue %s: %v", kind, err)
	}
}

func claim(t *testing.T, q JobQueueUnderTest) ports.Job {
	t.Helper()
	job, err := q.Claim(context.Background(), contractLease)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	return job
}

func wantNothingToClaim(t *testing.T, q JobQueueUnderTest, when string) {
	t.Helper()
	if job, err := q.Claim(context.Background(), contractLease); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Errorf("Claim %s = %+v, %v, want ErrNoJobAvailable", when, job, err)
	}
}

func wantLeaseLost(t *testing.T, op string, err error) {
	t.Helper()
	if !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("%s = %v, want ErrLeaseLost", op, err)
	}
}

func contractClaimOnAnEmptyQueueFindsNothing(t *testing.T, q JobQueueUnderTest) {
	wantNothingToClaim(t, q, "on an empty queue")
}

func contractAnEnqueuedJobIsClaimedOnceWithItsTrackUserAndKind(t *testing.T, q JobQueueUnderTest) {
	userID := shared.NewUserId(uuid.New())
	trackID := enqueued(t, q, userID)

	job := claim(t, q)

	if job.TrackID != trackID || job.UserID != userID || job.Kind != ports.JobKindAcquire || job.Run != 1 {
		t.Errorf("Claim = %+v, want track %s user %s kind acquire run 1", job, trackID, userID)
	}
	wantNothingToClaim(t, q, "while the lease is live")
}

func contractAJobEnqueuedWithADelayIsNotClaimableYet(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := q.AddTrack(t, shared.NewUserId(uuid.New()))
	if err := q.Enqueue(ctx, trackID, ports.JobKindAcquire, time.Hour); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	wantNothingToClaim(t, q, "before the delay elapsed")
}

func contractALapsedLeaseIsReclaimedUnderANewFence(t *testing.T, q JobQueueUnderTest) {
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	first := claim(t, q)

	q.ExpireLease(t, trackID)
	second := claim(t, q)

	if second.TrackID != trackID || second.Fence == first.Fence {
		t.Errorf("reclaim = %+v, want the same track under a fence other than %v", second, first.Fence)
	}
}

func contractAZombiesHeartbeatReleaseAndSettleFailAndLeaveTheNewOwnerAlone(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	zombie := claim(t, q)
	q.ExpireLease(t, trackID)
	owner := claim(t, q)

	wantLeaseLost(t, "zombie Heartbeat", q.Heartbeat(ctx, trackID, zombie.Fence, contractLease))
	wantLeaseLost(t, "zombie Release", q.Release(ctx, trackID, zombie.Fence, 0))
	wantLeaseLost(t, "zombie Settle", q.Settle(ctx, trackID, zombie.Fence))

	if err := q.Heartbeat(ctx, trackID, owner.Fence, contractLease); err != nil {
		t.Errorf("owner Heartbeat = %v, want nil", err)
	}
	if err := q.Settle(ctx, trackID, owner.Fence); err != nil {
		t.Errorf("owner Settle = %v, want nil", err)
	}
}

func contractHeartbeatWithTheOwningFenceKeepsTheLeaseLive(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	job := claim(t, q)

	if err := q.Heartbeat(ctx, trackID, job.Fence, time.Hour); err != nil {
		t.Fatalf("Heartbeat = %v, want nil", err)
	}

	wantNothingToClaim(t, q, "after a heartbeat")
}

func contractHeartbeatWithAnotherFenceIsLeaseLost(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	job := claim(t, q)

	wantLeaseLost(t, "Heartbeat with a stale fence", q.Heartbeat(ctx, trackID, job.Fence+1, contractLease))
}

func contractHeartbeatAfterTheLeaseExpiredIsLeaseLostEvenWithTheOwningFence(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	job := claim(t, q)

	q.ExpireLease(t, trackID)

	wantLeaseLost(t, "Heartbeat after expiry", q.Heartbeat(ctx, trackID, job.Fence, contractLease))
}

func contractHeartbeatAfterSettleIsLeaseLost(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	job := claim(t, q)
	if err := q.Settle(ctx, trackID, job.Fence); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	wantLeaseLost(t, "Heartbeat after Settle", q.Heartbeat(ctx, trackID, job.Fence, contractLease))
}

func contractReleaseWithADelayHidesTheJobAndReleaseWithoutOneReturnsIt(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	job := claim(t, q)

	if err := q.Release(ctx, trackID, job.Fence, time.Hour); err != nil {
		t.Fatalf("Release with a delay = %v, want nil", err)
	}
	wantNothingToClaim(t, q, "before the release delay elapsed")

	if err := q.Release(ctx, trackID, job.Fence, 0); err != nil {
		t.Fatalf("Release without a delay = %v, want nil", err)
	}
	if again := claim(t, q); again.TrackID != trackID {
		t.Errorf("Claim after Release = %+v, want track %s", again, trackID)
	}
}

func contractAnEnqueueOverALiveLeaseKeepsTheOwnersLeaseAndFence(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	owner := claim(t, q)

	enqueue(t, q, trackID, ports.JobKindAcquire)

	wantNothingToClaim(t, q, "while the owner's lease is live")
	if err := q.Heartbeat(ctx, trackID, owner.Fence, contractLease); err != nil {
		t.Errorf("owner Heartbeat after the re-enqueue = %v, want nil", err)
	}
}

func contractSettleAfterAnEnqueueOverALiveLeaseLeavesTheJobClaimable(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	owner := claim(t, q)
	enqueue(t, q, trackID, ports.JobKindAcquire)

	if err := q.Settle(ctx, trackID, owner.Fence); err != nil {
		t.Fatalf("owner Settle = %v, want nil", err)
	}

	if next := claim(t, q); next.TrackID != trackID {
		t.Errorf("Claim = %+v, want the re-enqueued track %s", next, trackID)
	}
}

func contractAnEnqueueOfAnotherKindOverALiveLeaseConflicts(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	claim(t, q)

	if err := q.Enqueue(ctx, trackID, ports.JobKindReplace, 0); !errors.Is(err, acqdomain.ErrJobKindConflict) {
		t.Errorf("Enqueue replace over a running acquire = %v, want ErrJobKindConflict", err)
	}
}

func contractAnEnqueueOverALapsedLeaseFencesOutTheStaleWorkerAndKeepsTheNewJob(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	stale := claim(t, q)
	q.ExpireLease(t, trackID)

	enqueue(t, q, trackID, ports.JobKindAcquire)

	wantLeaseLost(t, "stale Heartbeat", q.Heartbeat(ctx, trackID, stale.Fence, contractLease))
	wantLeaseLost(t, "stale Release", q.Release(ctx, trackID, stale.Fence, time.Hour))
	wantLeaseLost(t, "stale Settle", q.Settle(ctx, trackID, stale.Fence))
	fresh := claim(t, q)
	if fresh.TrackID != trackID || fresh.Fence == stale.Fence || fresh.Run != 1 {
		t.Errorf("Claim after the stale calls = %+v, want the new job on a new fence at run 1 (stale fence %v)", fresh, stale.Fence)
	}
}

func contractAStaleSettleAfterPriorClaimsAndAReenqueueIsLeaseLost(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, shared.NewUserId(uuid.New()))
	var stale ports.Job
	for range 3 {
		stale = claim(t, q)
		q.ExpireLease(t, trackID)
	}

	enqueue(t, q, trackID, ports.JobKindAcquire)

	wantLeaseLost(t, "stale Settle", q.Settle(ctx, trackID, stale.Fence))
	if fresh := claim(t, q); fresh.Run != 1 {
		t.Errorf("Run after the re-enqueue = %d, want a new run at 1", fresh.Run)
	}
}

func contractClaimPrefersTheUserWithNoLiveLease(t *testing.T, q JobQueueUnderTest) {
	busyUser := shared.NewUserId(uuid.New())
	idleUser := shared.NewUserId(uuid.New())
	busyFirst := enqueued(t, q, busyUser)
	busySecond := enqueued(t, q, busyUser)
	idleLast := enqueued(t, q, idleUser)

	if first := claim(t, q); first.TrackID != busyFirst {
		t.Fatalf("first Claim = %s, want the earliest %s", first.TrackID, busyFirst)
	}

	if second := claim(t, q); second.TrackID != idleLast {
		t.Errorf("second Claim = %s, want the idle user's %s ahead of the busy user's %s", second.TrackID, idleLast, busySecond)
	}
}

var contractCases = []struct {
	name string
	run  func(t *testing.T, q JobQueueUnderTest)
}{
	{"claim on an empty queue finds nothing", contractClaimOnAnEmptyQueueFindsNothing},
	{"an enqueued job is claimed once with its track user and kind", contractAnEnqueuedJobIsClaimedOnceWithItsTrackUserAndKind},
	{"a job enqueued with a delay is not claimable yet", contractAJobEnqueuedWithADelayIsNotClaimableYet},
	{"a lapsed lease is reclaimed under a new fence", contractALapsedLeaseIsReclaimedUnderANewFence},
	{"a zombie's heartbeat release and settle fail and leave the new owner alone", contractAZombiesHeartbeatReleaseAndSettleFailAndLeaveTheNewOwnerAlone},
	{"heartbeat with the owning fence keeps the lease live", contractHeartbeatWithTheOwningFenceKeepsTheLeaseLive},
	{"heartbeat with another fence is lease lost", contractHeartbeatWithAnotherFenceIsLeaseLost},
	{"heartbeat after the lease expired is lease lost even with the owning fence", contractHeartbeatAfterTheLeaseExpiredIsLeaseLostEvenWithTheOwningFence},
	{"heartbeat after settle is lease lost", contractHeartbeatAfterSettleIsLeaseLost},
	{"release with a delay hides the job and release without one returns it", contractReleaseWithADelayHidesTheJobAndReleaseWithoutOneReturnsIt},
	{"an enqueue over a live lease keeps the owner's lease and fence", contractAnEnqueueOverALiveLeaseKeepsTheOwnersLeaseAndFence},
	{"settle after an enqueue over a live lease leaves the job claimable", contractSettleAfterAnEnqueueOverALiveLeaseLeavesTheJobClaimable},
	{"an enqueue of another kind over a live lease conflicts", contractAnEnqueueOfAnotherKindOverALiveLeaseConflicts},
	{"an enqueue over a lapsed lease fences out the stale worker and keeps the new job", contractAnEnqueueOverALapsedLeaseFencesOutTheStaleWorkerAndKeepsTheNewJob},
	{"a stale settle after prior claims and a re-enqueue is lease lost", contractAStaleSettleAfterPriorClaimsAndAReenqueueIsLeaseLost},
	{"claim prefers the user with no live lease", contractClaimPrefersTheUserWithNoLiveLease},
}

func JobQueueContract(t *testing.T, newQueue func(t *testing.T) JobQueueUnderTest) {
	for _, c := range contractCases {
		t.Run(c.name, func(t *testing.T) { c.run(t, newQueue(t)) })
	}
}
