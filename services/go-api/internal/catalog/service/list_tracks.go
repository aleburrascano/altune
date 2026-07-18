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
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
}

type ListTracksOutput struct {
	Tracks  []*domain.Track
	Total   int
	HasMore bool
}

type ListTracksService struct {
	trackRepo trackLister
}

func NewListTracksService(trackRepo trackLister) *ListTracksService {
	return &ListTracksService{trackRepo: trackRepo}
}

func (s *ListTracksService) GetByID(ctx context.Context, userId shared.UserId, trackId domain.TrackId) (*domain.Track, error) {
	return s.trackRepo.GetByID(ctx, trackId, userId)
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
		HasMore: offset+len(tracks) < total,
	}, nil
}
