package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type trackNumberSetter interface {
	SetTrackNumber(ctx context.Context, id domain.TrackId, userId shared.UserId, trackNumber int) (updated bool, err error)
}

type SetTrackNumberService struct {
	trackRepo trackNumberSetter
}

func NewSetTrackNumberService(trackRepo trackNumberSetter) *SetTrackNumberService {
	return &SetTrackNumberService{trackRepo: trackRepo}
}

func (s *SetTrackNumberService) Execute(
	ctx context.Context,
	userId shared.UserId,
	trackId domain.TrackId,
	trackNumber int,
) (updated bool, err error) {
	if trackNumber <= 0 {
		return false, &domain.ValidationError{Message: "track_number must be positive"}
	}
	updated, err = s.trackRepo.SetTrackNumber(ctx, trackId, userId, trackNumber)
	if err != nil {
		return false, fmt.Errorf("set track number: %w", err)
	}
	return updated, nil
}
