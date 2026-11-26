package inmemoryqueue

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"cmp"
	"context"
	"fmt"
	"sync"
	"time"
)

type Queue struct {
	mu           sync.Mutex
	jobs         map[ids.TrackId]*job
	pendingUsers map[ids.TrackId]ids.UserId
	wake         chan<- struct{}
	now          func() time.Time
}

type job struct {
	userID      ids.UserId
	kind        ports.JobKind
	availableAt time.Time
	leaseUntil  time.Time
	fence       int
	run         int
	leased      bool
	reenqueued  bool

	correlationID string
}

var (
	_ ports.DownloadQueue    = (*Queue)(nil)
	_ ports.QueueDepthReader = (*Queue)(nil)
	_ ports.UpgradeJobQueue  = (*Queue)(nil)
)

func New(wake chan<- struct{}, now func() time.Time) *Queue {
	return &Queue{
		jobs:         make(map[ids.TrackId]*job),
		pendingUsers: make(map[ids.TrackId]ids.UserId),
		wake:         wake,
		now:          now,
	}
}

func (q *Queue) RememberUser(trackID ids.TrackId, userID ids.UserId) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if j, ok := q.jobs[trackID]; ok {
		j.userID = userID
		return
	}
	q.pendingUsers[trackID] = userID
}

func (q *Queue) Enqueue(ctx context.Context, trackID ids.TrackId, kind ports.JobKind, delay time.Duration) error {
	q.mu.Lock()
	if existing, queued := q.jobs[trackID]; queued && kind == ports.JobKindUpgrade && existing.kind != ports.JobKindUpgrade {
		q.mu.Unlock()
		return acqdomain.ErrJobKindConflict
	}
	j := q.jobOrNew(trackID)
	availableAt := q.now().Add(delay)
	correlationID := logging.CorrelationIDFromContext(ctx)
	if held, err := q.enqueueLive(j, kind, availableAt, correlationID); held {
		return err
	}
	j.correlationID = correlationID
	j.requeue(kind, availableAt)
	q.mu.Unlock()
	notifyWake(q.wake)
	return nil
}

func (j *job) requeue(kind ports.JobKind, availableAt time.Time) {
	if j.leased {
		j.fence++
	}
	j.kind, j.availableAt = kind, availableAt
	j.leased, j.reenqueued, j.run = false, false, 0
}

func (q *Queue) jobOrNew(trackID ids.TrackId) *job {
	j, ok := q.jobs[trackID]
	if ok {
		return j
	}
	j = &job{}
	if userID, pending := q.pendingUsers[trackID]; pending {
		j.userID = userID
		delete(q.pendingUsers, trackID)
	}
	q.jobs[trackID] = j
	return j
}

func (q *Queue) enqueueLive(j *job, kind ports.JobKind, availableAt time.Time, correlationID string) (held bool, err error) {
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
	j.correlationID = correlationID
	j.availableAt = availableAt
	q.mu.Unlock()
	notifyWake(q.wake)
	return true, nil
}

func (q *Queue) Claim(_ context.Context, lease time.Duration) (ports.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claimMatching(lease, isUserJob)
}

func (q *Queue) ClaimUpgrade(_ context.Context, lease time.Duration) (ports.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, userJob := q.findClaimable(q.now(), isUserJob); userJob != nil {
		return ports.Job{}, acqdomain.ErrNoJobAvailable
	}
	return q.claimMatching(lease, isUpgradeJob)
}

func isUserJob(j *job) bool { return j.kind != ports.JobKindUpgrade }

func isUpgradeJob(j *job) bool { return j.kind == ports.JobKindUpgrade }

func (q *Queue) claimMatching(lease time.Duration, match func(*job) bool) (ports.Job, error) {
	now := q.now()
	bestID, best := q.findClaimable(now, match)
	if best == nil {
		return ports.Job{}, acqdomain.ErrNoJobAvailable
	}
	best.leased = true
	best.leaseUntil = now.Add(lease)
	best.fence++
	best.run++
	best.reenqueued = false
	return ports.Job{
		TrackID: bestID, UserID: best.userID, Kind: best.kind, Run: best.run, ClaimNumber: ports.ClaimNumber(best.fence),
		Waited: max(now.Sub(best.availableAt), 0), CorrelationID: best.correlationID,
	}, nil
}

func (q *Queue) PendingDepth(_ context.Context) (int, time.Duration, error) {
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

func (q *Queue) findClaimable(now time.Time, match func(*job) bool) (ids.TrackId, *job) {
	var bestID ids.TrackId
	var best *job
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

func (q *Queue) claimsBefore(now time.Time, candidate, best *job) bool {
	byLiveLeases := cmp.Compare(q.liveLeases(now, candidate.userID), q.liveLeases(now, best.userID))
	return cmp.Or(byLiveLeases, candidate.availableAt.Compare(best.availableAt)) == -1
}

func (q *Queue) liveLeases(now time.Time, userID ids.UserId) int {
	live := 0
	for _, j := range q.jobs {
		if j.userID == userID && j.leased && now.Before(j.leaseUntil) {
			live++
		}
	}
	return live
}

func (q *Queue) Heartbeat(_ context.Context, trackID ids.TrackId, fence ports.ClaimNumber, lease time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return acqdomain.ErrLeaseLost
	}
	now := q.now()
	if !j.leased || ports.ClaimNumber(j.fence) != fence || !now.Before(j.leaseUntil) {
		return acqdomain.ErrLeaseLost
	}
	j.leaseUntil = now.Add(lease)
	return nil
}

func (q *Queue) VerifyLease(_ context.Context, trackID ids.TrackId, fence ports.ClaimNumber) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok || !j.leased || ports.ClaimNumber(j.fence) != fence || !q.now().Before(j.leaseUntil) {
		return acqdomain.ErrLeaseLost
	}
	return nil
}

func (q *Queue) Release(_ context.Context, trackID ids.TrackId, fence ports.ClaimNumber, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return fmt.Errorf("release acquisition job: track %s not found", trackID)
	}
	if ports.ClaimNumber(j.fence) != fence {
		return acqdomain.ErrLeaseLost
	}
	j.leased = false
	j.availableAt = q.now().Add(delay)
	j.reenqueued = false
	return nil
}

func (q *Queue) Settle(_ context.Context, trackID ids.TrackId, fence ports.ClaimNumber) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return nil
	}
	if ports.ClaimNumber(j.fence) != fence {
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
	select {
	case wake <- struct{}{}:
	default:
	}
}
