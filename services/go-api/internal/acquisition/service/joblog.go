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
	TrackID        string    `json:"track_id"`
	Title          string    `json:"title,omitempty"`
	Artist         string    `json:"artist,omitempty"`
	Album          string    `json:"album,omitempty"`
	SourceURL      string    `json:"source_url,omitempty"`
	ResolvedSource string    `json:"source,omitempty"`
	State          string    `json:"state"`
	Stage          string    `json:"stage,omitempty"`
	ScheduledAt    time.Time `json:"scheduled_at"`
	ElapsedMs      int64     `json:"elapsed_ms"`
	Reason         string    `json:"reason,omitempty"`
}

type jobLog struct {
	mu        sync.Mutex
	jobs      map[string]*JobRecord
	recent    []JobRecord
	succeeded atomic.Uint64
	failed    atomic.Uint64
}

func newJobLog() *jobLog {
	return &jobLog{jobs: make(map[string]*JobRecord)}
}

func (l *jobLog) register(trackID, sourceURL string) {
	l.mu.Lock()
	l.jobs[trackID] = &JobRecord{
		TrackID:     trackID,
		SourceURL:   sourceURL,
		State:       JobQueued,
		ScheduledAt: time.Now().UTC(),
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
	case "failed":
		l.failed.Add(1)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	j := l.jobs[trackID]
	if j == nil {
		j = &JobRecord{TrackID: trackID, ScheduledAt: time.Now().UTC()}
	}
	delete(l.jobs, trackID)
	j.State = state
	j.Reason = reason
	j.ElapsedMs = time.Since(j.ScheduledAt).Milliseconds()
	l.recent = append(l.recent, *j)
	if len(l.recent) > recentJobCap {
		l.recent = l.recent[len(l.recent)-recentJobCap:]
	}
}

func (l *jobLog) counts() (succeeded, failed uint64) {
	return l.succeeded.Load(), l.failed.Load()
}

func (l *jobLog) snapshot() (jobs []JobRecord, recent []JobRecord) {
	now := time.Now().UTC()

	l.mu.Lock()
	jobs = make([]JobRecord, 0, len(l.jobs))
	for _, j := range l.jobs {
		jr := *j
		jr.ElapsedMs = now.Sub(j.ScheduledAt).Milliseconds()
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
