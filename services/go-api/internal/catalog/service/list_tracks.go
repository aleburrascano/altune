package service

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

type ListTracksOutput struct {
	Tracks  []*domain.Track
	Total   int
	HasMore bool
}

type ListTracksService struct {
	trackRepo ports.TrackRepository
}

func NewListTracksService(trackRepo ports.TrackRepository) *ListTracksService {
	return &ListTracksService{trackRepo: trackRepo}
}

func (s *ListTracksService) Execute(ctx context.Context, userId shared.UserId, limit, offset int) (*ListTracksOutput, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	tracks, total, err := s.trackRepo.ListForUser(ctx, userId, limit, offset)
	if err != nil {
		return nil, err
	}

	return &ListTracksOutput{
		Tracks:  tracks,
		Total:   total,
		HasMore: offset+len(tracks) < total,
	}, nil
}
