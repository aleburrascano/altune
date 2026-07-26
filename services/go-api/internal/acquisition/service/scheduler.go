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

type AcquisitionStatus struct {
	InFlight   int         `json:"in_flight"`
	Succeeded  uint64      `json:"succeeded"`
	Failed     uint64      `json:"failed"`
	ActiveJobs []JobRecord `json:"jobs"`
	Recent     []JobRecord `json:"recent"`
}

type BackgroundAcquisitionScheduler struct {
	svc      *AcquireTrackAudioService
	events   events.Publisher
	wg       *sync.WaitGroup
	sem      chan struct{}
	cancel   context.CancelFunc
	baseCtx  context.Context
	closed   atomic.Bool
	inflight sync.Map

	inflightCount atomic.Int64

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

func WithSchedulerEvents(pub events.Publisher) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.events = pub }
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
			s.log.complete(key, "failed", err.Error())
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
		InFlight:   int(s.inflightCount.Load()),
		Succeeded:  succeeded,
		Failed:     failed,
		ActiveJobs: jobs,
		Recent:     recent,
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
