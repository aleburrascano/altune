package service

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

type GetTrackStatusService struct {
	trackRepo ports.TrackGetter
}

func NewGetTrackStatusService(trackRepo ports.TrackGetter) *GetTrackStatusService {
	return &GetTrackStatusService{trackRepo: trackRepo}
}

func (s *GetTrackStatusService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) (*domain.Track, error) {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return nil, wrapRepoError(ctx, "get track status", err)
	}
	if track == nil {
		return nil, ErrTrackNotFound
	}
	return track, nil
}
