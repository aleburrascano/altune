package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
)

type LibraryLensService struct {
	lensRepo ports.LibraryLensRepository
}

func NewLibraryLensService(lensRepo ports.LibraryLensRepository) *LibraryLensService {
	return &LibraryLensService{lensRepo: lensRepo}
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
	albums, err := s.lensRepo.ListAlbumsForUser(ctx, userId, query)
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
	artists, err := s.lensRepo.ListArtistsForUser(ctx, userId, query)
	if err != nil {
		return nil, fmt.Errorf("library artists: %w", err)
	}
	return artists, nil
}
