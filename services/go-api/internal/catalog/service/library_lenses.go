package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type libraryLensReader interface {
	ListAlbumsForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.AlbumGroup, error)
	ListArtistsForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.ArtistGroup, error)
}

type LibraryLensService struct {
	trackRepo libraryLensReader
}

func NewLibraryLensService(trackRepo libraryLensReader) *LibraryLensService {
	return &LibraryLensService{trackRepo: trackRepo}
}

func (s *LibraryLensService) Albums(
	ctx context.Context,
	userId shared.UserId,
	query domain.LibraryQuery,
) ([]domain.AlbumGroup, error) {
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
		return nil, &domain.ValidationError{Message: "artists cannot be sorted by year"}
	}
	artists, err := s.trackRepo.ListArtistsForUser(ctx, userId, query)
	if err != nil {
		return nil, fmt.Errorf("library artists: %w", err)
	}
	return artists, nil
}
