package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
)

type ListTracksOutput struct {
	Tracks  []*domain.Track
	Total   int
	Limit   int
	HasMore bool
}

type ListTracksService struct {
	lensRepo ports.LibraryLensRepository
}

func NewListTracksService(lensRepo ports.LibraryLensRepository) *ListTracksService {
	return &ListTracksService{lensRepo: lensRepo}
}

func (s *ListTracksService) Execute(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) (*ListTracksOutput, error) {
	if query.Offset < 0 {
		return nil, domain.NewValidationError("offset must not be negative")
	}
	query = clampLibraryLimit(query)

	tracks, total, err := s.lensRepo.ListFilteredForUser(ctx, userId, query)
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
