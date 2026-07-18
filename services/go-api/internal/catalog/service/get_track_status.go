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
	trackRepo trackReader
}

func NewGetTrackStatusService(trackRepo trackReader) *GetTrackStatusService {
	return &GetTrackStatusService{trackRepo: trackRepo}
}

func (s *GetTrackStatusService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) (*domain.Track, error) {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return nil, fmt.Errorf("get track status: %w", err)
	}
	return track, nil
}
