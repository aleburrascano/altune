package library

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
)

type LibraryLensService struct {
	lensRepo ports.LibraryLensRepository
}

func NewLibraryLensService(lensRepo ports.LibraryLensRepository) *LibraryLensService {
	return &LibraryLensService{lensRepo: lensRepo}
}

func (s *LibraryLensService) Albums(
	ctx context.Context,
	userId shared.UserId,
	query domain.LibraryQuery,
) ([]domain.AlbumGroup, error) {
	query, err := domain.NormalizeLibraryPage(query)
	if err != nil {
		return nil, err
	}
	albums, err := s.lensRepo.ListAlbumsForUser(ctx, userId, query)
	if err != nil {
		return nil, domain.WrapRepoError(ctx, "library albums", err)
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
	query, err := domain.NormalizeLibraryPage(query)
	if err != nil {
		return nil, err
	}
	artists, err := s.lensRepo.ListArtistsForUser(ctx, userId, query)
	if err != nil {
		return nil, domain.WrapRepoError(ctx, "library artists", err)
	}
	return artists, nil
}
