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

type AcquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId, sourceURL string)
}

type Shutdownable interface {
	Shutdown(ctx context.Context)
}

type BackgroundAcquisitionScheduler struct {
	svc       *AcquireTrackAudioService
	wg        *sync.WaitGroup
	sem       chan struct{}
	cancel    context.CancelFunc
	parentCtx context.Context
	closed    atomic.Bool
	inflight  sync.Map
}

func NewBackgroundAcquisitionScheduler(
	svc *AcquireTrackAudioService,
	wg *sync.WaitGroup,
	sem chan struct{},
) *BackgroundAcquisitionScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &BackgroundAcquisitionScheduler{
		svc:       svc,
		wg:        wg,
		sem:       sem,
		cancel:    cancel,
		parentCtx: ctx,
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
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.inflight.Delete(key)
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
		case <-s.parentCtx.Done():
			slog.Info("acquisition.cancelled_before_start", "track_id", key)
			return
		}

		if err := s.svc.Execute(s.parentCtx, userId, trackId, sourceURL); err != nil {
			slog.Error("background acquisition failed",
				"track_id", key, "error", err)
		}
	}()
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
