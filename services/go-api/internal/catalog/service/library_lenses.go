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

const defaultCatalogPageSize = 50

func clampPageSize(limit int) int {
	if limit <= 0 {
		return defaultCatalogPageSize
	}
	if limit > domain.MaxLibraryPageSize {
		return domain.MaxLibraryPageSize
	}
	return limit
}

func normalizePage(limit, offset int) (int, error) {
	if offset < 0 {
		return 0, domain.NewValidationError("offset must not be negative")
	}
	return clampPageSize(limit), nil
}

func normalizeLibraryPage(query domain.LibraryQuery) (domain.LibraryQuery, error) {
	limit, err := normalizePage(query.Limit, query.Offset)
	if err != nil {
		return domain.LibraryQuery{}, err
	}
	query.Limit = limit
	return query, nil
}

func (s *LibraryLensService) Albums(
	ctx context.Context,
	userId shared.UserId,
	query domain.LibraryQuery,
) ([]domain.AlbumGroup, error) {
	query, err := normalizeLibraryPage(query)
	if err != nil {
		return nil, err
	}
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
	query, err := normalizeLibraryPage(query)
	if err != nil {
		return nil, err
	}
	artists, err := s.lensRepo.ListArtistsForUser(ctx, userId, query)
	if err != nil {
		return nil, fmt.Errorf("library artists: %w", err)
	}
	return artists, nil
}
