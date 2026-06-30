package service

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type Shutdownable interface {
	Shutdown(ctx context.Context)
}

const (
	recentFailCap = 20
	recentJobCap  = 20
)

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

// AcquisitionStatus is the in-memory snapshot of the background acquisition
// pipeline for the operator console. In-memory only — resets on restart.
type AcquisitionStatus struct {
	InFlight    int             `json:"in_flight"`
	Succeeded   uint64          `json:"succeeded"`
	Failed      uint64          `json:"failed"`
	Jobs        []JobRecord     `json:"jobs"`            // current queued/running jobs
	Recent      []JobRecord     `json:"recent"`          // recent terminal outcomes, newest first
	RecentFails []FailureRecord `json:"recent_failures"` // failures only (retained for compatibility)
}

type BackgroundAcquisitionScheduler struct {
	svc      *AcquireTrackAudioService
	wg       *sync.WaitGroup
	sem      chan struct{}
	cancel   context.CancelFunc
	baseCtx  context.Context // owned lifecycle context (not a request ctx); cancelled on Shutdown
	closed   atomic.Bool
	inflight sync.Map

	// Operator-console telemetry (in-memory).
	inflightCount atomic.Int64
	succeeded     atomic.Uint64
	failed        atomic.Uint64
	failMu        sync.Mutex
	recentFails   []FailureRecord

	jobsMu sync.Mutex
	jobs   map[string]*JobRecord // current queued/running jobs, keyed by track id
	recent []JobRecord           // recent terminal outcomes, oldest→newest
}

func NewBackgroundAcquisitionScheduler(
	svc *AcquireTrackAudioService,
	wg *sync.WaitGroup,
	sem chan struct{},
) *BackgroundAcquisitionScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &BackgroundAcquisitionScheduler{
		svc:     svc,
		wg:      wg,
		sem:     sem,
		cancel:  cancel,
		baseCtx: ctx,
		jobs:    make(map[string]*JobRecord),
	}
}

func (s *BackgroundAcquisitionScheduler) Schedule(userId shared.UserId, trackId domain.TrackId, sourceURL string) {
	if s.closed.Load() {
		slog.Warn("schedule_after_shutdown", "track_id", trackId.String())
		return
	}

	key := trackId.String()
	if _, loaded := s.inflight.LoadOrStore(key, struct{}{}); loaded {
		slog.Info("schedule_skip_inflight", "track_id", key)
		return
	}

	slog.Info("acquisition.scheduling", "track_id", key, "user_id", userId.String())
	s.inflightCount.Add(1)
	s.registerJob(key, sourceURL)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.inflight.Delete(key)
		defer s.inflightCount.Add(-1)
		defer func() {
			if r := recover(); r != nil {
				s.completeJob(key, "failed", "panic")
				slog.Error("acquisition_panic",
					"track_id", key,
					"panic", r,
					"stack", string(debug.Stack()),
				)
			}
		}()

		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		case <-s.baseCtx.Done():
			s.completeJob(key, "cancelled", "")
			slog.Info("acquisition.cancelled_before_start", "track_id", key)
			return
		}

		s.markRunning(key)
		jobCtx := withJobReporter(s.baseCtx, schedulerJobReporter{s: s, trackID: key, userId: userId})
		if err := s.svc.Execute(jobCtx, userId, trackId, sourceURL); err != nil {
			s.failed.Add(1)
			s.recordFailure(key, err.Error())
			s.completeJob(key, "failed", err.Error())
			slog.Error("background acquisition failed",
				"track_id", key, "error", err)
			return
		}
		s.succeeded.Add(1)
		s.completeJob(key, "succeeded", "")
	}()
}

// registerJob records a newly scheduled job in the queued state.
func (s *BackgroundAcquisitionScheduler) registerJob(trackID, sourceURL string) {
	s.jobsMu.Lock()
	s.jobs[trackID] = &JobRecord{
		TrackID:     trackID,
		SourceURL:   sourceURL,
		State:       "queued",
		ScheduledAt: time.Now().UTC(),
	}
	s.jobsMu.Unlock()
}

// markRunning transitions a queued job to running once it acquires a worker slot.
func (s *BackgroundAcquisitionScheduler) markRunning(trackID string) {
	s.jobsMu.Lock()
	if j := s.jobs[trackID]; j != nil {
		j.State = "running"
	}
	s.jobsMu.Unlock()
}

// updateJob applies fn to the in-flight job record under the lock, if present.
func (s *BackgroundAcquisitionScheduler) updateJob(trackID string, fn func(*JobRecord)) {
	s.jobsMu.Lock()
	if j := s.jobs[trackID]; j != nil {
		fn(j)
	}
	s.jobsMu.Unlock()
}

// schedulerJobReporter is the per-job jobReporter the scheduler hands the acquire
// pipeline via context, so live metadata + stage transitions land on the job
// record the operator console reads.
type schedulerJobReporter struct {
	s       *BackgroundAcquisitionScheduler
	trackID string
	userId  shared.UserId
}

func (r schedulerJobReporter) meta(title, artist, album string) {
	r.s.updateJob(r.trackID, func(j *JobRecord) { j.Title, j.Artist, j.Album = title, artist, album })
}
func (r schedulerJobReporter) stage(name string) {
	r.s.updateJob(r.trackID, func(j *JobRecord) { j.Stage = name })
	// Push the stage transition to the user's event stream so the client can
	// show "Finding source… / Downloading… / Finishing up…" live. Guarded for
	// the eval/test paths that build the service without an event publisher.
	if r.s.svc != nil && r.s.svc.events != nil {
		r.s.svc.events.Publish(r.userId, "track_acquisition_progress", map[string]any{
			"track_id": r.trackID,
			"stage":    name,
		})
	}
}
func (r schedulerJobReporter) source(url string) {
	r.s.updateJob(r.trackID, func(j *JobRecord) {
		if j.Source == "" {
			j.Source = url
		}
	})
}

// completeJob moves a job to the recent-terminal ring with its outcome.
func (s *BackgroundAcquisitionScheduler) completeJob(trackID, state, reason string) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	j := s.jobs[trackID]
	if j == nil {
		j = &JobRecord{TrackID: trackID, ScheduledAt: time.Now().UTC()}
	}
	delete(s.jobs, trackID)
	j.State = state
	j.Reason = reason
	j.ElapsedMs = time.Since(j.ScheduledAt).Milliseconds()
	s.recent = append(s.recent, *j)
	if len(s.recent) > recentJobCap {
		s.recent = s.recent[len(s.recent)-recentJobCap:]
	}
}

func (s *BackgroundAcquisitionScheduler) recordFailure(trackID, reason string) {
	s.failMu.Lock()
	s.recentFails = append(s.recentFails, FailureRecord{TrackID: trackID, Reason: reason})
	if len(s.recentFails) > recentFailCap {
		s.recentFails = s.recentFails[len(s.recentFails)-recentFailCap:]
	}
	s.failMu.Unlock()
}

// Status returns the in-memory acquisition pipeline snapshot for the operator
// console.
func (s *BackgroundAcquisitionScheduler) Status() AcquisitionStatus {
	s.failMu.Lock()
	fails := make([]FailureRecord, len(s.recentFails))
	copy(fails, s.recentFails)
	s.failMu.Unlock()

	now := time.Now().UTC()
	s.jobsMu.Lock()
	jobs := make([]JobRecord, 0, len(s.jobs))
	for _, j := range s.jobs {
		jr := *j
		jr.ElapsedMs = now.Sub(j.ScheduledAt).Milliseconds()
		jobs = append(jobs, jr)
	}
	recent := make([]JobRecord, len(s.recent))
	for i, j := range s.recent { // newest first
		recent[len(s.recent)-1-i] = j
	}
	s.jobsMu.Unlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ScheduledAt.Before(jobs[j].ScheduledAt) })

	return AcquisitionStatus{
		InFlight:    int(s.inflightCount.Load()),
		Succeeded:   s.succeeded.Load(),
		Failed:      s.failed.Load(),
		Jobs:        jobs,
		Recent:      recent,
		RecentFails: fails,
	}
}

func (s *BackgroundAcquisitionScheduler) Shutdown(ctx context.Context) {
	s.closed.Store(true)
	s.cancel()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("background tasks drained")
	case <-ctx.Done():
		slog.Warn("background task drain timed out")
	}
}
