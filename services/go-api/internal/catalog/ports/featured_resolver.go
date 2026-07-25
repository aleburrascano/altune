package ports

import (
	"context"

	"altune/go-api/internal/catalog/domain"
)

type FeaturedArtistResolver interface {
	Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error)
}

func NoopFeaturedArtistResolver() FeaturedArtistResolver { return noopFeaturedArtistResolver{} }

type noopFeaturedArtistResolver struct{}

func (noopFeaturedArtistResolver) Resolve(_ context.Context, _, _ string) ([]domain.FeaturedArtist, error) {
	return nil, nil
}
