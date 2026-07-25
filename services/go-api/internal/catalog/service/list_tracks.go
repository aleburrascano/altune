package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type trackLister interface {
	ListFilteredForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) (tracks []*domain.Track, total int, err error)
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

func (s *ListTracksService) Execute(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) (*ListTracksOutput, error) {
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 2000 {
		query.Limit = 2000
	}

	tracks, total, err := s.trackRepo.ListFilteredForUser(ctx, userId, query)
	if err != nil {
		return nil, fmt.Errorf("list tracks: %w", err)
	}

	return &ListTracksOutput{
		Tracks:  tracks,
		Total:   total,
		Limit:   query.Limit,
		HasMore: query.Offset+len(tracks) < total,
	}, nil
}
