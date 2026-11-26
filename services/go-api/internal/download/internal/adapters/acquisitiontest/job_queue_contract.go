package acquisitiontest

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type JobQueueUnderTest interface {
	ports.DownloadQueue
	AddTrack(t *testing.T, userID ids.UserId) ids.TrackId
	ExpireLease(t *testing.T, trackID ids.TrackId)
}

const contractLease = time.Minute

func enqueued(t *testing.T, q JobQueueUnderTest, userID ids.UserId) ids.TrackId {
	t.Helper()
	trackID := q.AddTrack(t, userID)
	enqueue(t, q, trackID, ports.JobKindAcquire)
	return trackID
}

func enqueue(t *testing.T, q JobQueueUnderTest, trackID ids.TrackId, kind ports.JobKind) {
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
	userID := ids.NewUserId(uuid.New())
	trackID := enqueued(t, q, userID)

	job := claim(t, q)

	if job.TrackID != trackID || job.UserID != userID || job.Kind != ports.JobKindAcquire || job.Run != 1 {
		t.Errorf("Claim = %+v, want track %s user %s kind acquire run 1", job, trackID, userID)
	}
	wantNothingToClaim(t, q, "while the lease is live")
}

func contractAJobEnqueuedWithADelayIsNotClaimableYet(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := q.AddTrack(t, ids.NewUserId(uuid.New()))
	if err := q.Enqueue(ctx, trackID, ports.JobKindAcquire, time.Hour); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	wantNothingToClaim(t, q, "before the delay elapsed")
}

func contractALapsedLeaseIsReclaimedUnderANewFence(t *testing.T, q JobQueueUnderTest) {
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	first := claim(t, q)

	q.ExpireLease(t, trackID)
	second := claim(t, q)

	if second.TrackID != trackID || second.ClaimNumber == first.ClaimNumber {
		t.Errorf("reclaim = %+v, want the same track under a fence other than %v", second, first.ClaimNumber)
	}
}

func contractAZombiesHeartbeatReleaseAndSettleFailAndLeaveTheNewOwnerAlone(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	zombie := claim(t, q)
	q.ExpireLease(t, trackID)
	owner := claim(t, q)

	wantLeaseLost(t, "zombie Heartbeat", q.Heartbeat(ctx, trackID, zombie.ClaimNumber, contractLease))
	wantLeaseLost(t, "zombie Release", q.Release(ctx, trackID, zombie.ClaimNumber, 0))
	wantLeaseLost(t, "zombie Settle", q.Settle(ctx, trackID, zombie.ClaimNumber))

	if err := q.Heartbeat(ctx, trackID, owner.ClaimNumber, contractLease); err != nil {
		t.Errorf("owner Heartbeat = %v, want nil", err)
	}
	if err := q.Settle(ctx, trackID, owner.ClaimNumber); err != nil {
		t.Errorf("owner Settle = %v, want nil", err)
	}
}

func contractHeartbeatWithTheOwningFenceKeepsTheLeaseLive(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	job := claim(t, q)

	if err := q.Heartbeat(ctx, trackID, job.ClaimNumber, time.Hour); err != nil {
		t.Fatalf("Heartbeat = %v, want nil", err)
	}

	wantNothingToClaim(t, q, "after a heartbeat")
}

func contractHeartbeatWithAnotherFenceIsLeaseLost(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	job := claim(t, q)

	wantLeaseLost(t, "Heartbeat with a stale fence", q.Heartbeat(ctx, trackID, job.ClaimNumber+1, contractLease))
}

func contractHeartbeatAfterTheLeaseExpiredIsLeaseLostEvenWithTheOwningFence(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	job := claim(t, q)

	q.ExpireLease(t, trackID)

	wantLeaseLost(t, "Heartbeat after expiry", q.Heartbeat(ctx, trackID, job.ClaimNumber, contractLease))
}

func contractHeartbeatAfterSettleIsLeaseLost(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	job := claim(t, q)
	if err := q.Settle(ctx, trackID, job.ClaimNumber); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	wantLeaseLost(t, "Heartbeat after Settle", q.Heartbeat(ctx, trackID, job.ClaimNumber, contractLease))
}

func contractReleaseWithADelayHidesTheJobAndReleaseWithoutOneReturnsIt(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	job := claim(t, q)

	if err := q.Release(ctx, trackID, job.ClaimNumber, time.Hour); err != nil {
		t.Fatalf("Release with a delay = %v, want nil", err)
	}
	wantNothingToClaim(t, q, "before the release delay elapsed")

	if err := q.Release(ctx, trackID, job.ClaimNumber, 0); err != nil {
		t.Fatalf("Release without a delay = %v, want nil", err)
	}
	if again := claim(t, q); again.TrackID != trackID {
		t.Errorf("Claim after Release = %+v, want track %s", again, trackID)
	}
}

func contractAnEnqueueOverALiveLeaseKeepsTheOwnersLeaseAndFence(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	owner := claim(t, q)

	enqueue(t, q, trackID, ports.JobKindAcquire)

	wantNothingToClaim(t, q, "while the owner's lease is live")
	if err := q.Heartbeat(ctx, trackID, owner.ClaimNumber, contractLease); err != nil {
		t.Errorf("owner Heartbeat after the re-enqueue = %v, want nil", err)
	}
}

func contractSettleAfterAnEnqueueOverALiveLeaseLeavesTheJobClaimable(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	owner := claim(t, q)
	enqueue(t, q, trackID, ports.JobKindAcquire)

	if err := q.Settle(ctx, trackID, owner.ClaimNumber); err != nil {
		t.Fatalf("owner Settle = %v, want nil", err)
	}

	if next := claim(t, q); next.TrackID != trackID {
		t.Errorf("Claim = %+v, want the re-enqueued track %s", next, trackID)
	}
}

func contractAnEnqueueOfAnotherKindOverALiveLeaseConflicts(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	claim(t, q)

	if err := q.Enqueue(ctx, trackID, ports.JobKindReplace, 0); !errors.Is(err, acqdomain.ErrJobKindConflict) {
		t.Errorf("Enqueue replace over a running acquire = %v, want ErrJobKindConflict", err)
	}
}

func contractAnEnqueueOverALapsedLeaseFencesOutTheStaleWorkerAndKeepsTheNewJob(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	stale := claim(t, q)
	q.ExpireLease(t, trackID)

	enqueue(t, q, trackID, ports.JobKindAcquire)

	wantLeaseLost(t, "stale Heartbeat", q.Heartbeat(ctx, trackID, stale.ClaimNumber, contractLease))
	wantLeaseLost(t, "stale Release", q.Release(ctx, trackID, stale.ClaimNumber, time.Hour))
	wantLeaseLost(t, "stale Settle", q.Settle(ctx, trackID, stale.ClaimNumber))
	fresh := claim(t, q)
	if fresh.TrackID != trackID || fresh.ClaimNumber == stale.ClaimNumber || fresh.Run != 1 {
		t.Errorf("Claim after the stale calls = %+v, want the new job on a new fence at run 1 (stale fence %v)", fresh, stale.ClaimNumber)
	}
}

func contractAStaleSettleAfterPriorClaimsAndAReenqueueIsLeaseLost(t *testing.T, q JobQueueUnderTest) {
	ctx := context.Background()
	trackID := enqueued(t, q, ids.NewUserId(uuid.New()))
	var stale ports.Job
	for range 3 {
		stale = claim(t, q)
		q.ExpireLease(t, trackID)
	}

	enqueue(t, q, trackID, ports.JobKindAcquire)

	wantLeaseLost(t, "stale Settle", q.Settle(ctx, trackID, stale.ClaimNumber))
	if fresh := claim(t, q); fresh.Run != 1 {
		t.Errorf("Run after the re-enqueue = %d, want a new run at 1", fresh.Run)
	}
}

func contractClaimPrefersTheUserWithNoLiveLease(t *testing.T, q JobQueueUnderTest) {
	busyUser := ids.NewUserId(uuid.New())
	idleUser := ids.NewUserId(uuid.New())
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

func contractTheEnqueuingRequestsCorrelationIDTravelsWithEveryRunOfTheJob(t *testing.T, q JobQueueUnderTest) {
	trackID := q.AddTrack(t, ids.NewUserId(uuid.New()))
	ctx := logging.WithCorrelationID(context.Background(), "corr-contract")
	if err := q.Enqueue(ctx, trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	first := claim(t, q)
	if err := q.Release(context.Background(), trackID, first.ClaimNumber, 0); err != nil {
		t.Fatalf("Release: %v", err)
	}
	second := claim(t, q)

	if first.CorrelationID != "corr-contract" || second.CorrelationID != "corr-contract" {
		t.Errorf("correlation ids on the first and the retried claim = %q, %q, want %q on both", first.CorrelationID, second.CorrelationID, "corr-contract")
	}
}

func contractAJobEnqueuedWithoutACorrelationIDClaimsWithNone(t *testing.T, q JobQueueUnderTest) {
	trackID := q.AddTrack(t, ids.NewUserId(uuid.New()))
	if err := q.Enqueue(logging.WithCorrelationID(context.Background(), "corr-old"), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	first := claim(t, q)
	if err := q.Settle(context.Background(), trackID, first.ClaimNumber); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	enqueue(t, q, trackID, ports.JobKindAcquire)

	if got := claim(t, q).CorrelationID; got != "" {
		t.Errorf("correlation id on a job enqueued without one = %q, want none: the earlier request's id must not label this one", got)
	}
}

func contractAnAcceptedSecondEnqueueOfTheSameKindTakesTheLatestRequestsCorrelationID(t *testing.T, q JobQueueUnderTest) {
	trackID := q.AddTrack(t, ids.NewUserId(uuid.New()))
	for _, id := range []string{"corr-first", "corr-latest"} {
		if err := q.Enqueue(logging.WithCorrelationID(context.Background(), id), trackID, ports.JobKindAcquire, 0); err != nil {
			t.Fatalf("Enqueue under %s: %v", id, err)
		}
	}

	if got := claim(t, q).CorrelationID; got != "corr-latest" {
		t.Errorf("correlation id after two accepted enqueues = %q, want the latest request's %q", got, "corr-latest")
	}
}

func contractARefusedEnqueueLeavesTheRunningJobsCorrelationIDAlone(t *testing.T, q JobQueueUnderTest) {
	trackID := q.AddTrack(t, ids.NewUserId(uuid.New()))
	if err := q.Enqueue(logging.WithCorrelationID(context.Background(), "corr-running"), trackID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	running := claim(t, q)

	err := q.Enqueue(logging.WithCorrelationID(context.Background(), "corr-refused"), trackID, ports.JobKindReplace, 0)
	if !errors.Is(err, acqdomain.ErrJobKindConflict) {
		t.Fatalf("Enqueue of another kind over a live lease = %v, want ErrJobKindConflict", err)
	}
	if err := q.Release(context.Background(), trackID, running.ClaimNumber, 0); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if got := claim(t, q).CorrelationID; got != "corr-running" {
		t.Errorf("correlation id after a refused enqueue = %q, want the running job's %q", got, "corr-running")
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
	{"the enqueuing request's correlation id travels with every run of the job", contractTheEnqueuingRequestsCorrelationIDTravelsWithEveryRunOfTheJob},
	{"a job enqueued without a correlation id claims with none", contractAJobEnqueuedWithoutACorrelationIDClaimsWithNone},
	{"an accepted second enqueue of the same kind takes the latest request's correlation id", contractAnAcceptedSecondEnqueueOfTheSameKindTakesTheLatestRequestsCorrelationID},
	{"a refused enqueue leaves the running job's correlation id alone", contractARefusedEnqueueLeavesTheRunningJobsCorrelationIDAlone},
}

func JobQueueContract(t *testing.T, newQueue func(t *testing.T) JobQueueUnderTest) {
	for _, c := range contractCases {
		t.Run(c.name, func(t *testing.T) { c.run(t, newQueue(t)) })
	}
}
