package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
)

type GetTrackStatusService struct {
	trackRepo ports.TrackGetter
}

func NewGetTrackStatusService(trackRepo ports.TrackGetter) *GetTrackStatusService {
	return &GetTrackStatusService{trackRepo: trackRepo}
}

func (s *GetTrackStatusService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) (*domain.Track, error) {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return nil, wrapRepoError(ctx, "get track status", err)
	}
	if track == nil {
		return nil, ErrTrackNotFound
	}
	return track, nil
}

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
	query, err := normalizeLibraryPage(query)
	if err != nil {
		return nil, err
	}

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
