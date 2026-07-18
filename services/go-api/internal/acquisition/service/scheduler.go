package service

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
)

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

// BackgroundAcquisitionScheduler runs acquisition jobs on goroutines gated by a
// semaphore, deduping in-flight work per track. Job execution lives here; the
// operator-console telemetry those jobs produce lives in jobLog.
type BackgroundAcquisitionScheduler struct {
	svc      *AcquireTrackAudioService
	events   events.Publisher
	wg       *sync.WaitGroup
	sem      chan struct{}
	cancel   context.CancelFunc
	baseCtx  context.Context // owned lifecycle context (not a request ctx); cancelled on Shutdown
	closed   atomic.Bool
	inflight sync.Map

	// Operator-console counters (in-memory).
	inflightCount atomic.Int64
	succeeded     atomic.Uint64
	failed        atomic.Uint64

	log *jobLog
}

func NewBackgroundAcquisitionScheduler(
	svc *AcquireTrackAudioService,
	wg *sync.WaitGroup,
	sem chan struct{},
	opts ...func(*BackgroundAcquisitionScheduler),
) *BackgroundAcquisitionScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &BackgroundAcquisitionScheduler{
		svc:     svc,
		wg:      wg,
		sem:     sem,
		cancel:  cancel,
		baseCtx: ctx,
		log:     newJobLog(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithSchedulerEvents wires the publisher the per-job reporter uses for
// track_acquisition_progress events. Without it (eval/test paths) stage
// transitions only land on the operator-console job record.
func WithSchedulerEvents(pub events.Publisher) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.events = pub }
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
	s.log.register(key, sourceURL)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.inflight.Delete(key)
		defer s.inflightCount.Add(-1)
		defer func() {
			if r := recover(); r != nil {
				s.log.complete(key, "failed", "panic")
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
			s.log.complete(key, "cancelled", "")
			slog.Info("acquisition.cancelled_before_start", "track_id", key)
			return
		}

		s.log.markRunning(key)
		jobCtx := withJobReporter(s.baseCtx, schedulerJobReporter{
			log: s.log, events: s.events, trackID: key, userId: userId,
		})
		if err := s.svc.Execute(jobCtx, userId, trackId, sourceURL); err != nil {
			s.failed.Add(1)
			s.log.complete(key, "failed", err.Error())
			slog.Error("background acquisition failed",
				"track_id", key, "error", err)
			return
		}
		s.succeeded.Add(1)
		s.log.complete(key, "succeeded", "")
	}()
}

// schedulerJobReporter is the per-job jobReporter the scheduler hands the acquire
// pipeline via context, so live metadata + stage transitions land on the job
// record the operator console reads and on the user's event stream.
type schedulerJobReporter struct {
	log     *jobLog
	events  events.Publisher
	trackID string
	userId  shared.UserId
}

func (r schedulerJobReporter) meta(title, artist, album string) {
	r.log.update(r.trackID, func(j *JobRecord) { j.Title, j.Artist, j.Album = title, artist, album })
}
func (r schedulerJobReporter) stage(name string) {
	r.log.update(r.trackID, func(j *JobRecord) { j.Stage = name })
	// Push the stage transition to the user's event stream so the client can
	// show "Finding source… / Downloading… / Finishing up…" live. Guarded for
	// the eval/test paths that build the scheduler without an event publisher.
	if r.events != nil {
		r.events.Publish(r.userId, "track_acquisition_progress", map[string]any{
			"track_id": r.trackID,
			"stage":    name,
		})
	}
}
func (r schedulerJobReporter) source(url string) {
	r.log.update(r.trackID, func(j *JobRecord) {
		if j.Source == "" {
			j.Source = url
		}
	})
}

// Status returns the in-memory acquisition pipeline snapshot for the operator
// console.
func (s *BackgroundAcquisitionScheduler) Status() AcquisitionStatus {
	jobs, recent, fails := s.log.snapshot()
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
