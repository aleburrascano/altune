package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

type LibraryLensService struct {
	trackRepo ports.TrackRepository
}

func NewLibraryLensService(trackRepo ports.TrackRepository) *LibraryLensService {
	return &LibraryLensService{trackRepo: trackRepo}
}

func clampLibraryLimit(query domain.LibraryQuery) domain.LibraryQuery {
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 2000 {
		query.Limit = 2000
	}
	return query
}

func (s *LibraryLensService) Albums(
	ctx context.Context,
	userId shared.UserId,
	query domain.LibraryQuery,
) ([]domain.AlbumGroup, error) {
	query = clampLibraryLimit(query)
	albums, err := s.trackRepo.ListAlbumsForUser(ctx, userId, query)
	if err != nil {
		return nil, fmt.Errorf("library albums: %w", err)
	}
	return albums, nil
}

func (s *LibraryLensService) Artists(
	ctx context.Context,
	userId shared.UserId,
	query domain.LibraryQuery,
) ([]domain.ArtistGroup, error) {
	if query.Sort == domain.SortYear {
		return nil, domain.NewValidationError("artists cannot be sorted by year")
	}
	query = clampLibraryLimit(query)
	artists, err := s.trackRepo.ListArtistsForUser(ctx, userId, query)
	if err != nil {
		return nil, fmt.Errorf("library artists: %w", err)
	}
	return artists, nil
}
