package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// trackLister is the narrow read this service actually calls, out of
// ports.TrackRepository's full surface.
type trackLister interface {
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
}

type ListTracksOutput struct {
	Tracks  []*domain.Track
	Total   int
	Limit   int
	HasMore bool
}

type ListTracksService struct {
	trackRepo trackLister
}

func NewListTracksService(trackRepo trackLister) *ListTracksService {
	return &ListTracksService{trackRepo: trackRepo}
}

func (s *ListTracksService) Execute(ctx context.Context, userId shared.UserId, limit, offset int) (*ListTracksOutput, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 2000 {
		limit = 2000
	}

	tracks, total, err := s.trackRepo.ListForUser(ctx, userId, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list tracks: %w", err)
	}

	return &ListTracksOutput{
		Tracks:  tracks,
		Total:   total,
		Limit:   limit,
		HasMore: offset+len(tracks) < total,
	}, nil
}
