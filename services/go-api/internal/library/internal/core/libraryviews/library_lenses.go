package libraryviews

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
)

type LibraryViewService struct {
	lensRepo ports.LibraryViewRepository
}

func NewLibraryLensService(lensRepo ports.LibraryViewRepository) *LibraryViewService {
	return &LibraryViewService{lensRepo: lensRepo}
}

func (s *LibraryViewService) Albums(
	ctx context.Context,
	userId ids.UserId,
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

func (s *LibraryViewService) Artists(
	ctx context.Context,
	userId ids.UserId,
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
