package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"sync"
	"time"
)

type memJobQueue struct {
	mu           sync.Mutex
	jobs         map[domain.TrackId]*memJob
	pendingUsers map[domain.TrackId]shared.UserId
	wake         chan<- struct{}
	now          func() time.Time
}

type memJob struct {
	userID      shared.UserId
	kind        ports.JobKind
	availableAt time.Time
	leaseUntil  time.Time
	attempts    int
	run         int
	leased      bool
	reenqueued  bool
}

var _ ports.JobQueue = (*memJobQueue)(nil)
var _ ports.QueueDepthReader = (*memJobQueue)(nil)

func newMemJobQueue(wake chan<- struct{}) *memJobQueue {
	return newMemJobQueueWithClock(wake, time.Now)
}

func newMemJobQueueWithClock(wake chan<- struct{}, now func() time.Time) *memJobQueue {
	return &memJobQueue{
		jobs:         make(map[domain.TrackId]*memJob),
		pendingUsers: make(map[domain.TrackId]shared.UserId),
		wake:         wake,
		now:          now,
	}
}

func (q *memJobQueue) rememberUser(trackID domain.TrackId, userID shared.UserId) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if j, ok := q.jobs[trackID]; ok {
		j.userID = userID
		return
	}
	q.pendingUsers[trackID] = userID
}

func (q *memJobQueue) Enqueue(_ context.Context, trackID domain.TrackId, kind ports.JobKind, availableAt time.Time) error {
	q.mu.Lock()
	j := q.jobOrNew(trackID)
	if held, err := q.enqueueLive(j, kind, availableAt); held {
		return err
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

func (q *memJobQueue) jobOrNew(trackID domain.TrackId) *memJob {
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
	if running != kind {
		q.mu.Unlock()
		return true, ports.ErrJobKindConflict
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

	now := q.now()
	bestID, best := q.findClaimable(now)
	if best == nil {
		return ports.Job{}, ports.ErrNoJobAvailable
	}
	best.leased = true
	best.leaseUntil = now.Add(lease)
	best.attempts++
	best.run++
	best.reenqueued = false
	return ports.Job{TrackID: bestID, UserID: best.userID, Kind: best.kind, Attempts: best.attempts, Run: best.run, Fence: ports.Fence(best.attempts)}, nil
}

func (q *memJobQueue) PendingDepth(_ context.Context) (int, time.Duration, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now()
	pending := 0
	var oldest time.Time
	for _, j := range q.jobs {
		if j.leased && now.Before(j.leaseUntil) {
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

func (q *memJobQueue) findClaimable(now time.Time) (domain.TrackId, *memJob) {
	var bestID domain.TrackId
	var best *memJob
	for id, j := range q.jobs {
		if j.leased && now.Before(j.leaseUntil) {
			continue
		}
		if now.Before(j.availableAt) {
			continue
		}
		if best == nil || j.availableAt.Before(best.availableAt) {
			best, bestID = j, id
		}
	}
	return bestID, best
}

func (q *memJobQueue) Heartbeat(_ context.Context, trackID domain.TrackId, fence ports.Fence, lease time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return fmt.Errorf("heartbeat acquisition job: track %s not found", trackID)
	}
	if !j.leased || ports.Fence(j.attempts) != fence {
		return ports.ErrLeaseLost
	}
	j.leaseUntil = q.now().Add(lease)
	return nil
}

func (q *memJobQueue) Release(_ context.Context, trackID domain.TrackId, fence ports.Fence, availableAt time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return fmt.Errorf("release acquisition job: track %s not found", trackID)
	}
	if ports.Fence(j.attempts) != fence {
		return ports.ErrLeaseLost
	}
	j.leased = false
	j.availableAt = availableAt
	j.reenqueued = false
	return nil
}

func (q *memJobQueue) Settle(_ context.Context, trackID domain.TrackId, fence ports.Fence) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[trackID]
	if !ok {
		return nil
	}
	if ports.Fence(j.attempts) != fence {
		return ports.ErrLeaseLost
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
