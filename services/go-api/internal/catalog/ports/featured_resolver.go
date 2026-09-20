package ports

import (
	"altune/go-api/internal/catalog/domain"
	"context"
)

type FeaturedArtistResolver interface {
	Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error)
}
