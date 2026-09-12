package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
)

type ListFeaturingService struct {
	featuredRepo ports.FeaturedArtistRepository
}

func NewListFeaturingService(featuredRepo ports.FeaturedArtistRepository) *ListFeaturingService {
	return &ListFeaturingService{featuredRepo: featuredRepo}
}

func (s *ListFeaturingService) Execute(
	ctx context.Context,
	userId shared.UserId,
	fa domain.FeaturedArtist,
) ([]*domain.Track, error) {
	tracks, err := s.featuredRepo.ListTracksFeaturing(ctx, userId, fa)
	if err != nil {
		return nil, fmt.Errorf("list featuring: %w", err)
	}
	return tracks, nil
}
