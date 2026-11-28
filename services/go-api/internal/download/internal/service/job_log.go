package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"sort"
	"sync"
	"sync/atomic"
)

const RecentJobCap = 20

type jobLog struct {
	now       clock.Func
	mu        sync.Mutex
	jobs      map[ids.TrackId]*domain.JobRecord
	recent    []domain.JobRecord
	succeeded atomic.Uint64
	failed    atomic.Uint64
}

func newJobLog(now clock.Func) *jobLog {
	return &jobLog{now: now, jobs: make(map[ids.TrackId]*domain.JobRecord)}
}

func (l *jobLog) register(trackID ids.TrackId, sourceURL string) {
	l.mu.Lock()
	l.jobs[trackID] = &domain.JobRecord{
		TrackID:     trackID.String(),
		SourceURL:   sourceURL,
		State:       domain.JobQueued,
		ScheduledAt: l.now(),
	}
	l.mu.Unlock()
}

func (l *jobLog) markRunning(trackID ids.TrackId) {
	l.mu.Lock()
	if j := l.jobs[trackID]; j != nil {
		j.State = domain.JobRunning
	}
	l.mu.Unlock()
}

func (l *jobLog) update(trackID ids.TrackId, fn func(*domain.JobRecord)) {
	l.mu.Lock()
	if j := l.jobs[trackID]; j != nil {
		fn(j)
	}
	l.mu.Unlock()
}

func (l *jobLog) countCompletion(state domain.JobState) {
	switch state {
	case domain.JobSucceeded:
		l.succeeded.Add(1)
	case domain.JobFailed:
		l.failed.Add(1)
	case domain.JobQueued, domain.JobRunning, domain.JobCancelled, domain.JobRetrying, domain.JobKept:
	}
}

func (l *jobLog) complete(trackID ids.TrackId, state domain.JobState, reason string) domain.JobRecord {
	l.countCompletion(state)

	l.mu.Lock()
	defer l.mu.Unlock()
	j := l.jobs[trackID]
	if j == nil {
		j = &domain.JobRecord{TrackID: trackID.String(), ScheduledAt: l.now()}
	}
	delete(l.jobs, trackID)
	j.State = state
	j.Reason = reason
	j.ElapsedMs = l.now().Sub(j.ScheduledAt).Milliseconds()
	l.recent = append(l.recent, *j)
	if len(l.recent) > RecentJobCap {
		l.recent = l.recent[len(l.recent)-RecentJobCap:]
	}
	return *j
}

func (l *jobLog) counts() (succeeded, failed uint64) {
	return l.succeeded.Load(), l.failed.Load()
}

func (l *jobLog) snapshot() (jobs, recent []domain.JobRecord) {
	l.mu.Lock()
	jobs = make([]domain.JobRecord, 0, len(l.jobs))
	for _, j := range l.jobs {
		jr := *j
		jr.ElapsedMs = l.now().Sub(j.ScheduledAt).Milliseconds()
		jobs = append(jobs, jr)
	}
	recent = make([]domain.JobRecord, len(l.recent))
	for i, j := range l.recent {
		recent[len(l.recent)-1-i] = j
	}
	l.mu.Unlock()

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ScheduledAt.Before(jobs[j].ScheduledAt) })
	return jobs, recent
}
