package service

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
)

type GetTrackStatusService struct {
	trackRepo ports.TrackGetter
}

func NewGetTrackStatusService(trackRepo ports.TrackGetter) *GetTrackStatusService {
	return &GetTrackStatusService{trackRepo: trackRepo}
}

func (s *GetTrackStatusService) Execute(ctx context.Context, userId ids.UserId, trackId domain.TrackId) (*domain.Track, error) {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return nil, domain.ErrTrackNotFound
	}
	if err != nil {
		return nil, wrapRepoError(ctx, "get track status", err)
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
	lensRepo ports.LibraryViewRepository
}

func NewListTracksService(lensRepo ports.LibraryViewRepository) *ListTracksService {
	return &ListTracksService{lensRepo: lensRepo}
}

func (s *ListTracksService) Execute(ctx context.Context, userId ids.UserId, query domain.LibraryQuery) (*ListTracksOutput, error) {
	query, err := domain.NormalizeLibraryPage(query)
	if err != nil {
		return nil, fmt.Errorf("normalize library page: %w", err)
	}

	tracks, total, err := s.lensRepo.ListFilteredForUser(ctx, userId, query)
	if err != nil {
		return nil, wrapRepoError(ctx, "list tracks", err)
	}

	return &ListTracksOutput{
		Tracks:  tracks,
		Total:   total,
		Limit:   query.Limit,
		HasMore: query.Offset+len(tracks) < total,
	}, nil
}
