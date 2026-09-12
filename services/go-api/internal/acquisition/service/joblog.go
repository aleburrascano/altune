package service

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const recentJobCap = 20

const (
	JobQueued    = "queued"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
)

type JobRecord struct {
	TrackID        string
	Title          string
	Artist         string
	Album          string
	SourceURL      string
	ResolvedSource string
	State          string
	Stage          string
	ScheduledAt    time.Time
	ElapsedMs      int64
	Reason         string
	Provenance     string
}

type jobLog struct {
	mu        sync.Mutex
	jobs      map[string]*JobRecord
	recent    []JobRecord
	succeeded atomic.Uint64
	failed    atomic.Uint64
	// now stamps ScheduledAt with a monotonic-bearing instant; since measures
	// elapsed from it. Both are monotonic-safe (immune to wall-clock jumps) in
	// production and injectable so tests can simulate clock steps. UTC is
	// applied only at the serialization edge, never to the instant used here.
	now   func() time.Time
	since func(time.Time) time.Duration
}

func newJobLog() *jobLog {
	return newJobLogWithClock(time.Now, time.Since)
}

func newJobLogWithClock(now func() time.Time, since func(time.Time) time.Duration) *jobLog {
	return &jobLog{jobs: make(map[string]*JobRecord), now: now, since: since}
}

func (l *jobLog) register(trackID, sourceURL string) {
	l.mu.Lock()
	l.jobs[trackID] = &JobRecord{
		TrackID:     trackID,
		SourceURL:   sourceURL,
		State:       JobQueued,
		ScheduledAt: l.now(),
	}
	l.mu.Unlock()
}

func (l *jobLog) markRunning(trackID string) {
	l.mu.Lock()
	if j := l.jobs[trackID]; j != nil {
		j.State = JobRunning
	}
	l.mu.Unlock()
}

func (l *jobLog) update(trackID string, fn func(*JobRecord)) {
	l.mu.Lock()
	if j := l.jobs[trackID]; j != nil {
		fn(j)
	}
	l.mu.Unlock()
}

func (l *jobLog) complete(trackID, state, reason string) {
	switch state {
	case JobSucceeded:
		l.succeeded.Add(1)
	case JobFailed:
		l.failed.Add(1)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	j := l.jobs[trackID]
	if j == nil {
		j = &JobRecord{TrackID: trackID, ScheduledAt: l.now()}
	}
	delete(l.jobs, trackID)
	j.State = state
	j.Reason = reason
	j.ElapsedMs = l.since(j.ScheduledAt).Milliseconds()
	l.recent = append(l.recent, *j)
	if len(l.recent) > recentJobCap {
		l.recent = l.recent[len(l.recent)-recentJobCap:]
	}
}

func (l *jobLog) counts() (succeeded, failed uint64) {
	return l.succeeded.Load(), l.failed.Load()
}

func (l *jobLog) snapshot() (jobs []JobRecord, recent []JobRecord) {
	l.mu.Lock()
	jobs = make([]JobRecord, 0, len(l.jobs))
	for _, j := range l.jobs {
		jr := *j
		jr.ElapsedMs = l.since(j.ScheduledAt).Milliseconds()
		jobs = append(jobs, jr)
	}
	recent = make([]JobRecord, len(l.recent))
	for i, j := range l.recent {
		recent[len(l.recent)-1-i] = j
	}
	l.mu.Unlock()

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ScheduledAt.Before(jobs[j].ScheduledAt) })
	return jobs, recent
}
