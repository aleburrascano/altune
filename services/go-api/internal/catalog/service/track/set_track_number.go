package track

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/internal/core/ports"
	shared "altune/go-api/internal/shared/kernel"
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
	userId shared.UserId,
	trackId domain.TrackId,
	trackNumber int,
) (updated bool, err error) {
	if trackNumber <= 0 {
		return false, domain.NewValidationError("track_number must be positive")
	}
	if trackNumber > maxTrackNumber {
		return false, domain.NewValidationError("track_number exceeds maximum (int4)")
	}
	updated, err = s.trackRepo.SetTrackNumber(ctx, trackId, userId, trackNumber)
	if err != nil {
		return false, domain.WrapRepoError(ctx, "set track number", err)
	}
	if updated {
		return true, nil
	}
	return false, s.requireOwnedTrack(ctx, userId, trackId)
}

func (s *SetTrackNumberService) requireOwnedTrack(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	_, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return domain.ErrTrackNotFound
	}
	if err != nil {
		return fmt.Errorf("set track number: lookup: %w", err)
	}
	return nil
}
