package service

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
)

type SetTrackNumberService struct {
	trackRepo ports.TrackNumberFiller
}

func NewSetTrackNumberService(trackRepo ports.TrackNumberFiller) *SetTrackNumberService {
	return &SetTrackNumberService{trackRepo: trackRepo}
}

func (s *SetTrackNumberService) Execute(
	ctx context.Context,
	userId ids.UserId,
	trackId domain.TrackId,
	trackNumber int,
) (updated bool, err error) {
	if err := domain.ValidateTrackNumber(trackNumber); err != nil {
		return false, fmt.Errorf("validate track number: %w", err)
	}
	updated, err = s.trackRepo.SetTrackNumber(ctx, trackId, userId, trackNumber)
	if err != nil {
		return false, wrapRepoError(ctx, "set track number", err)
	}
	if updated {
		return true, nil
	}
	return false, s.requireOwnedTrack(ctx, userId, trackId)
}

func (s *SetTrackNumberService) requireOwnedTrack(ctx context.Context, userId ids.UserId, trackId domain.TrackId) error {
	_, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return domain.ErrTrackNotFound
	}
	if err != nil {
		return fmt.Errorf("set track number: lookup: %w", err)
	}
	return nil
}
