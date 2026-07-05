package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

// ListFeaturingService returns the user's tracks that credit a given featured
// artist — the "everything featuring X" browse.
type ListFeaturingService struct {
	trackRepo ports.TrackRepository
}

func NewListFeaturingService(trackRepo ports.TrackRepository) *ListFeaturingService {
	return &ListFeaturingService{trackRepo: trackRepo}
}

func (s *ListFeaturingService) Execute(
	ctx context.Context,
	userId shared.UserId,
	fa domain.FeaturedArtist,
) ([]*domain.Track, error) {
	tracks, err := s.trackRepo.ListTracksFeaturing(ctx, userId, fa)
	if err != nil {
		return nil, fmt.Errorf("list featuring: %w", err)
	}
	return tracks, nil
}
