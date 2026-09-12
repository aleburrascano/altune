package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

type AcquisitionVerification struct {
	Ffprobe bool
	Ffmpeg  bool
	Fpcalc  bool
}

func (v AcquisitionVerification) FullyArmed() bool {
	return v.Ffprobe && v.Ffmpeg && v.Fpcalc
}

type AcquisitionStatus struct {
	InFlight     int
	Succeeded    uint64
	Failed       uint64
	Rejected     uint64
	Verification AcquisitionVerification
	ActiveJobs   []JobRecord
	Recent       []JobRecord
}

// defaultQueueDepthFactor bounds total outstanding acquisition jobs (in-flight
// + pending) at this multiple of the worker concurrency. A burst of Schedule
// calls past that bound is reported as rejected instead of spawning an
// unbounded number of goroutines and job-log entries.
const defaultQueueDepthFactor = 4

type BackgroundAcquisitionScheduler struct {
	svc      *AcquireTrackAudioService
	events   events.Publisher
	wg       *sync.WaitGroup
	sem      chan struct{}
	admit    chan struct{}
	cancel   context.CancelFunc
	baseCtx  context.Context
	closed   atomic.Bool
	inflight sync.Map

	queueDepth int

	inflightCount atomic.Int64
	rejected      atomic.Uint64

	verification AcquisitionVerification
	log          *jobLog
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
	depth := s.queueDepth
	if depth <= 0 {
		depth = cap(sem) * defaultQueueDepthFactor
	}
	if depth < 1 {
		depth = 1
	}
	s.admit = make(chan struct{}, depth)
	return s
}

func WithSchedulerEvents(pub events.Publisher) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.events = pub }
}

// WithQueueDepth caps the total number of outstanding acquisition jobs
// (in-flight + pending). Schedule calls past the cap are rejected rather than
// admitted. A non-positive value falls back to defaultQueueDepthFactor times
// the worker concurrency.
func WithQueueDepth(depth int) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.queueDepth = depth }
}

func WithVerificationStatus(v AcquisitionVerification) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		s.verification = v
		if !v.FullyArmed() {
			slog.Warn("acquisition.verification_degraded",
				"ffprobe", v.Ffprobe, "ffmpeg", v.Ffmpeg, "fpcalc", v.Fpcalc)
			return
		}
		slog.Info("acquisition.verification_armed")
	}
}

func (s *BackgroundAcquisitionScheduler) ScheduleReplace(userId shared.UserId, trackId domain.TrackId) {
	s.schedule(userId, trackId, "", true)
}

func (s *BackgroundAcquisitionScheduler) Schedule(userId shared.UserId, trackId domain.TrackId, sourceURL string) {
	s.schedule(userId, trackId, sourceURL, false)
}

func (s *BackgroundAcquisitionScheduler) schedule(
	userId shared.UserId,
	trackId domain.TrackId,
	sourceURL string,
	replace bool,
) {
	if s.closed.Load() {
		slog.Warn("schedule_after_shutdown", "track_id", trackId.String())
		return
	}

	key := trackId.String()
	if _, loaded := s.inflight.LoadOrStore(key, struct{}{}); loaded {
		slog.Info("schedule_skip_inflight", "track_id", key)
		return
	}

	// Bound arrival: acquire an admission slot synchronously before registering
	// or spawning anything. When the queue (in-flight + pending) is full, report
	// the job as rejected and release the dedup key so it can be retried later.
	select {
	case s.admit <- struct{}{}:
	default:
		s.inflight.Delete(key)
		s.rejected.Add(1)
		slog.Warn("acquisition.queue_full",
			"track_id", key, "queue_depth", cap(s.admit))
		return
	}

	slog.Info("acquisition.scheduling", "track_id", key, "user_id", userId.String())
	s.inflightCount.Add(1)
	s.log.register(key, sourceURL)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.admit }()
		defer s.inflight.Delete(key)
		defer s.inflightCount.Add(-1)
		defer func() {
			if r := recover(); r != nil {
				s.log.complete(key, JobFailed, "panic")
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
			s.log.complete(key, JobCancelled, "")
			slog.Info("acquisition.cancelled_before_start", "track_id", key)
			return
		}

		s.log.markRunning(key)
		jobCtx := withJobReporter(s.baseCtx, schedulerJobReporter{
			log: s.log, events: s.events, trackID: key, userId: userId,
		})
		run := s.svc.Execute
		if replace {
			run = s.svc.ExecuteReplace
		}
		if err := run(jobCtx, userId, trackId); err != nil {
			s.log.complete(key, JobFailed, err.Error())
			slog.Error("background acquisition failed",
				"track_id", key, "error", err)
			return
		}
		s.log.complete(key, JobSucceeded, "")
	}()
}

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
	if r.events != nil {
		r.events.Publish(r.userId, "track_acquisition_progress", map[string]any{
			"track_id": r.trackID,
			"stage":    name,
		})
	}
}

func (r schedulerJobReporter) provenance(value string) {
	r.log.update(r.trackID, func(j *JobRecord) { j.Provenance = value })
}

func (r schedulerJobReporter) source(url string) {
	r.log.update(r.trackID, func(j *JobRecord) {
		if j.ResolvedSource == "" {
			j.ResolvedSource = url
		}
	})
}

func (s *BackgroundAcquisitionScheduler) Status() AcquisitionStatus {
	jobs, recent := s.log.snapshot()
	succeeded, failed := s.log.counts()
	return AcquisitionStatus{
		InFlight:     int(s.inflightCount.Load()),
		Succeeded:    succeeded,
		Failed:       failed,
		Rejected:     s.rejected.Load(),
		Verification: s.verification,
		ActiveJobs:   jobs,
		Recent:       recent,
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
