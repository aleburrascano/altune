package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// GetTrackStatusService returns a single track for the authenticated user,
// used by the status-polling endpoint.
type GetTrackStatusService struct {
	trackRepo trackByIDGetter
}

func NewGetTrackStatusService(trackRepo trackByIDGetter) *GetTrackStatusService {
	return &GetTrackStatusService{trackRepo: trackRepo}
}

func (s *GetTrackStatusService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) (*domain.Track, error) {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return nil, fmt.Errorf("get track status: %w", err)
	}
	if track == nil {
		return nil, ErrTrackNotFound
	}
	return track, nil
}
