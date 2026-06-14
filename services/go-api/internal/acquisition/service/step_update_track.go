package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
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
	track, err := s.trackRepo.GetByID(ctx, s.trackId, s.userId)
	if err != nil {
		return fmt.Errorf("get track for update: %w", err)
	}
	if track == nil {
		return fmt.Errorf("track not found for update")
	}

	if err := track.MarkReady(ac.AudioRef); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}

	if err := s.trackRepo.Update(ctx, track); err != nil {
		return fmt.Errorf("persist track update: %w", err)
	}

	return nil
}

func (s *UpdateTrackStep) Rollback(ctx context.Context, _ *AcquisitionContext) error {
	track, err := s.trackRepo.GetByID(ctx, s.trackId, s.userId)
	if err != nil || track == nil {
		return err
	}

	track.RevertToPending()
	return s.trackRepo.Update(ctx, track)
}
