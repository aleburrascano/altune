package memory

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"cmp"
	"context"
	"fmt"
	"sync"
	"time"
)

func NewQueueFactory() func(wake chan<- struct{}, now clock.Func) ports.DownloadQueue {
	return func(wake chan<- struct{}, now clock.Func) ports.DownloadQueue { return New(wake, now) }
}

type Queue struct {
	mu           sync.Mutex
	jobs         map[ids.TrackId]*job
	pendingUsers map[ids.TrackId]ids.UserId
	wake         chan<- struct{}
	now          func() time.Time
}

type job struct {
	userID      ids.UserId
	kind        domain.JobKind
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

func (q *Queue) Enqueue(ctx context.Context, trackID ids.TrackId, kind domain.JobKind, delay time.Duration) error {
	q.mu.Lock()
	if existing, queued := q.jobs[trackID]; queued && kind == domain.JobKindUpgrade && existing.kind != domain.JobKindUpgrade {
		q.mu.Unlock()
		return domain.ErrJobKindConflict
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

func (j *job) requeue(kind domain.JobKind, availableAt time.Time) {
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

func (q *Queue) enqueueLive(j *job, kind domain.JobKind, availableAt time.Time, correlationID string) (held bool, err error) {
	if !j.leased || !q.now().Before(j.leaseUntil) {
		return false, nil
	}
	running := j.kind
	if running == domain.JobKindUpgrade && kind != domain.JobKindUpgrade {
		return false, nil
	}
	if running != kind {
		q.mu.Unlock()
		return true, domain.ErrJobKindConflict
	}
	j.reenqueued = true
	j.run = 0
	j.correlationID = correlationID
	j.availableAt = availableAt
	q.mu.Unlock()
	notifyWake(q.wake)
	return true, nil
}

func (q *Queue) Claim(_ context.Context, lease time.Duration) (domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claimMatching(lease, isUserJob)
}

func (q *Queue) ClaimUpgrade(_ context.Context, lease time.Duration) (domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, userJob := q.findClaimable(q.now(), isUserJob); userJob != nil {
		return domain.Job{}, domain.ErrNoJobAvailable
	}
	return q.claimMatching(lease, isUpgradeJob)
}

func isUserJob(j *job) bool { return j.kind != domain.JobKindUpgrade }

func isUpgradeJob(j *job) bool { return j.kind == domain.JobKindUpgrade }

func (q *Queue) claimMatching(lease time.Duration, match func(*job) bool) (domain.Job, error) {
	now := q.now()
	bestID, best := q.findClaimable(now, match)
	if best == nil {
		return domain.Job{}, domain.ErrNoJobAvailable
	}
	best.leased = true
	best.leaseUntil = now.Add(lease)
	best.fence++
	best.run++
	best.reenqueued = false
	return domain.Job{
		TrackID: bestID, UserID: best.userID, Kind: best.kind, Run: best.run, ClaimNumber: domain.ClaimNumber(best.fence),
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

func (q *Queue) Heartbeat(_ context.Context, trackID ids.TrackId, fence domain.ClaimNumber, lease time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return domain.ErrLeaseLost
	}
	now := q.now()
	if !j.leased || domain.ClaimNumber(j.fence) != fence || !now.Before(j.leaseUntil) {
		return domain.ErrLeaseLost
	}
	j.leaseUntil = now.Add(lease)
	return nil
}

func (q *Queue) VerifyLease(_ context.Context, trackID ids.TrackId, fence domain.ClaimNumber) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok || !j.leased || domain.ClaimNumber(j.fence) != fence || !q.now().Before(j.leaseUntil) {
		return domain.ErrLeaseLost
	}
	return nil
}

func (q *Queue) Release(_ context.Context, trackID ids.TrackId, fence domain.ClaimNumber, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return fmt.Errorf("release acquisition job: track %s not found", trackID)
	}
	if domain.ClaimNumber(j.fence) != fence {
		return domain.ErrLeaseLost
	}
	j.leased = false
	j.availableAt = q.now().Add(delay)
	j.reenqueued = false
	return nil
}

func (q *Queue) Settle(_ context.Context, trackID ids.TrackId, fence domain.ClaimNumber) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return nil
	}
	if domain.ClaimNumber(j.fence) != fence {
		return domain.ErrLeaseLost
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
