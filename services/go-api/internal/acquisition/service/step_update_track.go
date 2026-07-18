package service

import (
	"context"
	"errors"
	"fmt"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type UpdateTrackStep struct {
	trackRepo ports.TrackRepository
	userId    shared.UserId
	trackId   domain.TrackId
}

func NewUpdateTrackStep(trackRepo ports.TrackRepository, userId shared.UserId, trackId domain.TrackId) *UpdateTrackStep {
	return &UpdateTrackStep{
		trackRepo: trackRepo,
		userId:    userId,
		trackId:   trackId,
	}
}

func (s *UpdateTrackStep) Name() string { return "update_track" }

func (s *UpdateTrackStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	return loadAndUpdate(ctx, s.trackRepo, s.trackId, s.userId, errors.New("track not found for update"), func(track *domain.Track) error {
		if err := track.MarkReady(ac.AudioRef); err != nil {
			return fmt.Errorf("mark ready: %w", err)
		}
		if ac.Selected != nil && ac.Selected.Duration > 0 {
			track.SetDuration(ac.Selected.Duration)
		}
		return nil
	})
}

func (s *UpdateTrackStep) Rollback(ctx context.Context, _ *AcquisitionContext) error {
	return loadAndUpdate(ctx, s.trackRepo, s.trackId, s.userId, nil, func(track *domain.Track) error {
		track.RevertToPending()
		return nil
	})
}
