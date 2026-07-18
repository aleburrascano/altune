package service

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const recentJobCap = 20

// JobRecord is one acquisition job's lifecycle as the scheduler observes it:
// queued → running → succeeded/failed/cancelled. Title/Artist/Album and Stage are
// filled live by the pipeline via the job reporter. In-memory only.
type JobRecord struct {
	TrackID     string    `json:"track_id"`
	Title       string    `json:"title,omitempty"`
	Artist      string    `json:"artist,omitempty"`
	Album       string    `json:"album,omitempty"`
	SourceURL   string    `json:"source_url,omitempty"`
	Source      string    `json:"source,omitempty"` // resolved download source (selected candidate)
	State       string    `json:"state"`            // queued | running | succeeded | failed | cancelled
	Stage       string    `json:"stage,omitempty"`  // current pipeline step: search|select|download|tag|store|update_track
	ScheduledAt time.Time `json:"scheduled_at"`
	ElapsedMs   int64     `json:"elapsed_ms"`
	Reason      string    `json:"reason,omitempty"`
}

// jobLog is the scheduler's operator-console job telemetry: current
// queued/running jobs, running succeeded/failed totals, and a bounded ring of
// recent terminal outcomes (failures included, carrying their reason). complete
// is the single call site that advances all three, so they cannot drift apart.
// In-memory only — resets on restart.
type jobLog struct {
	mu        sync.Mutex
	jobs      map[string]*JobRecord // current queued/running jobs, keyed by track id
	recent    []JobRecord           // recent terminal outcomes, oldest→newest
	succeeded atomic.Uint64
	failed    atomic.Uint64
}

func newJobLog() *jobLog {
	return &jobLog{jobs: make(map[string]*JobRecord)}
}

// register records a newly scheduled job in the queued state.
func (l *jobLog) register(trackID, sourceURL string) {
	l.mu.Lock()
	l.jobs[trackID] = &JobRecord{
		TrackID:     trackID,
		SourceURL:   sourceURL,
		State:       "queued",
		ScheduledAt: time.Now().UTC(),
	}
	l.mu.Unlock()
}

// markRunning transitions a queued job to running once it acquires a worker slot.
func (l *jobLog) markRunning(trackID string) {
	l.mu.Lock()
	if j := l.jobs[trackID]; j != nil {
		j.State = "running"
	}
	l.mu.Unlock()
}

// update applies fn to the in-flight job record under the lock, if present.
func (l *jobLog) update(trackID string, fn func(*JobRecord)) {
	l.mu.Lock()
	if j := l.jobs[trackID]; j != nil {
		fn(j)
	}
	l.mu.Unlock()
}

// complete moves a job to the recent-terminal ring with its outcome and, for
// succeeded/failed outcomes, advances the matching running total.
func (l *jobLog) complete(trackID, state, reason string) {
	switch state {
	case "succeeded":
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

// counts returns the running succeeded/failed totals.
func (l *jobLog) counts() (succeeded, failed uint64) {
	return l.succeeded.Load(), l.failed.Load()
}

// snapshot returns copies of the current jobs (scheduled-first, with live
// elapsed times) and recent terminal outcomes (newest first). Failed jobs ride
// on the recent ring, carrying their reason — there is no separate failure ring.
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
	for i, j := range l.recent { // newest first
		recent[len(l.recent)-1-i] = j
	}
	l.mu.Unlock()

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ScheduledAt.Before(jobs[j].ScheduledAt) })
	return jobs, recent
}
