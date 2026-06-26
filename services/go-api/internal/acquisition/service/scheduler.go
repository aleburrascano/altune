package service

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type Shutdownable interface {
	Shutdown(ctx context.Context)
}

const recentFailCap = 20

// FailureRecord is one recent acquisition failure, surfaced on the operator
// console.
type FailureRecord struct {
	TrackID string `json:"track_id"`
	Reason  string `json:"reason"`
}

// AcquisitionStatus is the in-memory snapshot of the background acquisition
// pipeline for the operator console. In-memory only — resets on restart.
type AcquisitionStatus struct {
	InFlight    int             `json:"in_flight"`
	Succeeded   uint64          `json:"succeeded"`
	Failed      uint64          `json:"failed"`
	RecentFails []FailureRecord `json:"recent_failures"`
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
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.inflight.Delete(key)
		defer s.inflightCount.Add(-1)
		defer func() {
			if r := recover(); r != nil {
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
			slog.Info("acquisition.cancelled_before_start", "track_id", key)
			return
		}

		if err := s.svc.Execute(s.baseCtx, userId, trackId, sourceURL); err != nil {
			s.failed.Add(1)
			s.recordFailure(key, err.Error())
			slog.Error("background acquisition failed",
				"track_id", key, "error", err)
			return
		}
		s.succeeded.Add(1)
	}()
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

	return AcquisitionStatus{
		InFlight:    int(s.inflightCount.Load()),
		Succeeded:   s.succeeded.Load(),
		Failed:      s.failed.Load(),
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
