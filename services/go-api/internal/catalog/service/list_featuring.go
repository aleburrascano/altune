package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// featuringLister is the narrow read this service actually calls, out of
// ports.TrackRepository's full surface.
type featuringLister interface {
	ListTracksFeaturing(ctx context.Context, userId shared.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error)
}

// ListFeaturingService returns the user's tracks that credit a given featured
// artist — the "everything featuring X" browse.
type ListFeaturingService struct {
	trackRepo featuringLister
}

func NewListFeaturingService(trackRepo featuringLister) *ListFeaturingService {
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
