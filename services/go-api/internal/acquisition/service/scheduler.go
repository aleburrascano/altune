package service

import (
	"context"
	"log/slog"
	"sync"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type AcquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId)
}

type BackgroundAcquisitionScheduler struct {
	svc *AcquireTrackAudioService
	wg  *sync.WaitGroup
	sem chan struct{}
}

func NewBackgroundAcquisitionScheduler(
	svc *AcquireTrackAudioService,
	wg *sync.WaitGroup,
	sem chan struct{},
) *BackgroundAcquisitionScheduler {
	return &BackgroundAcquisitionScheduler{svc: svc, wg: wg, sem: sem}
}

func (s *BackgroundAcquisitionScheduler) Schedule(userId shared.UserId, trackId domain.TrackId) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		s.sem <- struct{}{}
		defer func() { <-s.sem }()

		ctx := context.Background()
		if err := s.svc.Execute(ctx, userId, trackId); err != nil {
			slog.Error("background acquisition failed",
				"track_id", trackId.String(), "error", err)
		}
	}()
}
