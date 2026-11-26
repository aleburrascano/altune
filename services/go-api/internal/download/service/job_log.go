package service

import (
	"altune/go-api/internal/download/internal/core/ports"
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
	jobs      map[ids.TrackId]*ports.JobRecord
	recent    []ports.JobRecord
	succeeded atomic.Uint64
	failed    atomic.Uint64
}

func newJobLog(now clock.Func) *jobLog {
	return &jobLog{now: now, jobs: make(map[ids.TrackId]*ports.JobRecord)}
}

func (l *jobLog) register(trackID ids.TrackId, sourceURL string) {
	l.mu.Lock()
	l.jobs[trackID] = &ports.JobRecord{
		TrackID:     trackID.String(),
		SourceURL:   sourceURL,
		State:       ports.JobQueued,
		ScheduledAt: l.now(),
	}
	l.mu.Unlock()
}

func (l *jobLog) markRunning(trackID ids.TrackId) {
	l.mu.Lock()
	if j := l.jobs[trackID]; j != nil {
		j.State = ports.JobRunning
	}
	l.mu.Unlock()
}

func (l *jobLog) update(trackID ids.TrackId, fn func(*ports.JobRecord)) {
	l.mu.Lock()
	if j := l.jobs[trackID]; j != nil {
		fn(j)
	}
	l.mu.Unlock()
}

func (l *jobLog) countCompletion(state ports.JobState) {
	switch state {
	case ports.JobSucceeded:
		l.succeeded.Add(1)
	case ports.JobFailed:
		l.failed.Add(1)
	case ports.JobQueued, ports.JobRunning, ports.JobCancelled, ports.JobRetrying, ports.JobKept:
	}
}

func (l *jobLog) complete(trackID ids.TrackId, state ports.JobState, reason string) ports.JobRecord {
	l.countCompletion(state)

	l.mu.Lock()
	defer l.mu.Unlock()
	j := l.jobs[trackID]
	if j == nil {
		j = &ports.JobRecord{TrackID: trackID.String(), ScheduledAt: l.now()}
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

func (l *jobLog) snapshot() (jobs, recent []ports.JobRecord) {
	l.mu.Lock()
	jobs = make([]ports.JobRecord, 0, len(l.jobs))
	for _, j := range l.jobs {
		jr := *j
		jr.ElapsedMs = l.now().Sub(j.ScheduledAt).Milliseconds()
		jobs = append(jobs, jr)
	}
	recent = make([]ports.JobRecord, len(l.recent))
	for i, j := range l.recent {
		recent[len(l.recent)-1-i] = j
	}
	l.mu.Unlock()

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ScheduledAt.Before(jobs[j].ScheduledAt) })
	return jobs, recent
}
