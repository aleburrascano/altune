package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"sync"
	"time"
)

type memJobQueue struct {
	mu           sync.Mutex
	jobs         map[shared.TrackId]*memJob
	pendingUsers map[shared.TrackId]shared.UserId
	wake         chan<- struct{}
	now          func() time.Time
}

type memJob struct {
	userID      shared.UserId
	kind        ports.JobKind
	availableAt time.Time
	leaseUntil  time.Time
	attempts    int
	fence       int
	run         int
	leased      bool
	reenqueued  bool
}

var (
	_ ports.JobQueue         = (*memJobQueue)(nil)
	_ ports.QueueDepthReader = (*memJobQueue)(nil)
	_ ports.UpgradeJobQueue  = (*memJobQueue)(nil)
)

func newMemJobQueue(wake chan<- struct{}) *memJobQueue {
	return newMemJobQueueWithClock(wake, time.Now)
}

func newMemJobQueueWithClock(wake chan<- struct{}, now func() time.Time) *memJobQueue {
	return &memJobQueue{
		jobs:         make(map[shared.TrackId]*memJob),
		pendingUsers: make(map[shared.TrackId]shared.UserId),
		wake:         wake,
		now:          now,
	}
}

func (q *memJobQueue) rememberUser(trackID shared.TrackId, userID shared.UserId) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if j, ok := q.jobs[trackID]; ok {
		j.userID = userID
		return
	}
	q.pendingUsers[trackID] = userID
}

func (q *memJobQueue) Enqueue(_ context.Context, trackID shared.TrackId, kind ports.JobKind, delay time.Duration) error {
	q.mu.Lock()
	if existing, queued := q.jobs[trackID]; queued && kind == ports.JobKindUpgrade && existing.kind != ports.JobKindUpgrade {
		q.mu.Unlock()
		return acqdomain.ErrJobKindConflict
	}
	j := q.jobOrNew(trackID)
	availableAt := q.now().Add(delay)
	if held, err := q.enqueueLive(j, kind, availableAt); held {
		return err
	}
	if j.leased {
		j.fence++
	}
	j.kind = kind
	j.availableAt = availableAt
	j.leased = false
	j.reenqueued = false
	j.run = 0
	q.mu.Unlock()
	notifyWake(q.wake)
	return nil
}

func (q *memJobQueue) jobOrNew(trackID shared.TrackId) *memJob {
	j, ok := q.jobs[trackID]
	if ok {
		return j
	}
	j = &memJob{}
	if userID, pending := q.pendingUsers[trackID]; pending {
		j.userID = userID
		delete(q.pendingUsers, trackID)
	}
	q.jobs[trackID] = j
	return j
}

func (q *memJobQueue) enqueueLive(j *memJob, kind ports.JobKind, availableAt time.Time) (held bool, err error) {
	if !j.leased || !q.now().Before(j.leaseUntil) {
		return false, nil
	}
	running := j.kind
	if running == ports.JobKindUpgrade && kind != ports.JobKindUpgrade {
		return false, nil
	}
	if running != kind {
		q.mu.Unlock()
		return true, acqdomain.ErrJobKindConflict
	}
	j.reenqueued = true
	j.run = 0
	j.availableAt = availableAt
	q.mu.Unlock()
	notifyWake(q.wake)
	return true, nil
}

func (q *memJobQueue) Claim(_ context.Context, lease time.Duration) (ports.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claimMatching(lease, isUserJob)
}

func (q *memJobQueue) ClaimUpgrade(_ context.Context, lease time.Duration) (ports.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, userJob := q.findClaimable(q.now(), isUserJob); userJob != nil {
		return ports.Job{}, acqdomain.ErrNoJobAvailable
	}
	return q.claimMatching(lease, isUpgradeJob)
}

func isUserJob(j *memJob) bool { return j.kind != ports.JobKindUpgrade }

func isUpgradeJob(j *memJob) bool { return j.kind == ports.JobKindUpgrade }

func (q *memJobQueue) claimMatching(lease time.Duration, match func(*memJob) bool) (ports.Job, error) {
	now := q.now()
	bestID, best := q.findClaimable(now, match)
	if best == nil {
		return ports.Job{}, acqdomain.ErrNoJobAvailable
	}
	best.leased = true
	best.leaseUntil = now.Add(lease)
	best.attempts++
	best.fence++
	best.run++
	best.reenqueued = false
	return ports.Job{
		TrackID: bestID, UserID: best.userID, Kind: best.kind, Run: best.run, Fence: ports.Fence(best.fence),
		Waited: max(now.Sub(best.availableAt), 0),
	}, nil
}

func (q *memJobQueue) PendingDepth(_ context.Context) (int, time.Duration, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now()
	pending := 0
	var oldest time.Time
	for _, j := range q.jobs {
		if isUpgradeJob(j) || (j.leased && now.Before(j.leaseUntil)) {
			continue
		}
		pending++
		if !now.Before(j.availableAt) && (oldest.IsZero() || j.availableAt.Before(oldest)) {
			oldest = j.availableAt
		}
	}
	if oldest.IsZero() {
		return pending, 0, nil
	}
	return pending, now.Sub(oldest), nil
}

func (q *memJobQueue) findClaimable(now time.Time, match func(*memJob) bool) (shared.TrackId, *memJob) {
	var bestID shared.TrackId
	var best *memJob
	for id, j := range q.jobs {
		if !match(j) {
			continue
		}
		if j.leased && now.Before(j.leaseUntil) {
			continue
		}
		if now.Before(j.availableAt) {
			continue
		}
		if best == nil || q.claimsBefore(now, j, best) {
			best, bestID = j, id
		}
	}
	return bestID, best
}

func (q *memJobQueue) claimsBefore(now time.Time, candidate, best *memJob) bool {
	candidateLive, bestLive := q.liveLeases(now, candidate.userID), q.liveLeases(now, best.userID)
	if candidateLive != bestLive {
		return candidateLive < bestLive
	}
	return candidate.availableAt.Before(best.availableAt)
}

func (q *memJobQueue) liveLeases(now time.Time, userID shared.UserId) int {
	live := 0
	for _, j := range q.jobs {
		if j.userID == userID && j.leased && now.Before(j.leaseUntil) {
			live++
		}
	}
	return live
}

func (q *memJobQueue) Heartbeat(_ context.Context, trackID shared.TrackId, fence ports.Fence, lease time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return acqdomain.ErrLeaseLost
	}
	now := q.now()
	if !j.leased || ports.Fence(j.fence) != fence || !now.Before(j.leaseUntil) {
		return acqdomain.ErrLeaseLost
	}
	j.leaseUntil = now.Add(lease)
	return nil
}

func (q *memJobQueue) VerifyLease(_ context.Context, trackID shared.TrackId, fence ports.Fence) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok || !j.leased || ports.Fence(j.fence) != fence || !q.now().Before(j.leaseUntil) {
		return acqdomain.ErrLeaseLost
	}
	return nil
}

func (q *memJobQueue) Release(_ context.Context, trackID shared.TrackId, fence ports.Fence, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return fmt.Errorf("release acquisition job: track %s not found", trackID)
	}
	if ports.Fence(j.fence) != fence {
		return acqdomain.ErrLeaseLost
	}
	j.leased = false
	j.availableAt = q.now().Add(delay)
	j.reenqueued = false
	return nil
}

func (q *memJobQueue) Settle(_ context.Context, trackID shared.TrackId, fence ports.Fence) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return nil
	}
	if ports.Fence(j.fence) != fence {
		return acqdomain.ErrLeaseLost
	}
	if j.reenqueued {
		j.leased = false
		j.reenqueued = false
		return nil
	}
	delete(q.jobs, trackID)
	return nil
}

func notifyWake(wake chan<- struct{}) {
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}
