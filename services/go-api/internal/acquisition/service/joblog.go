package service

import (
	"sort"
	"sync"
	"time"
)

const recentJobCap = 20

// FailureRecord is one recent acquisition failure, surfaced on the operator
// console.
type FailureRecord struct {
	TrackID string `json:"track_id"`
	Reason  string `json:"reason"`
}

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
// queued/running jobs plus a bounded ring of recent terminal outcomes (failures
// included, carrying their reason). One mutex owns both; nothing here touches job
// execution. In-memory only — resets on restart.
type jobLog struct {
	mu     sync.Mutex
	jobs   map[string]*JobRecord // current queued/running jobs, keyed by track id
	recent []JobRecord           // recent terminal outcomes, oldest→newest
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

// complete moves a job to the recent-terminal ring with its outcome.
func (l *jobLog) complete(trackID, state, reason string) {
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

// snapshot returns copies of the current jobs (scheduled-first, with live
// elapsed times), recent terminal outcomes (newest first), and the failures
// among them derived from the same ring (oldest→newest).
func (l *jobLog) snapshot() (jobs []JobRecord, recent []JobRecord, fails []FailureRecord) {
	now := time.Now().UTC()

	l.mu.Lock()
	jobs = make([]JobRecord, 0, len(l.jobs))
	for _, j := range l.jobs {
		jr := *j
		jr.ElapsedMs = now.Sub(j.ScheduledAt).Milliseconds()
		jobs = append(jobs, jr)
	}
	recent = make([]JobRecord, len(l.recent))
	fails = make([]FailureRecord, 0)
	for i, j := range l.recent { // newest first
		recent[len(l.recent)-1-i] = j
		if j.State == "failed" {
			fails = append(fails, FailureRecord{TrackID: j.TrackID, Reason: j.Reason})
		}
	}
	l.mu.Unlock()

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ScheduledAt.Before(jobs[j].ScheduledAt) })
	return jobs, recent, fails
}
