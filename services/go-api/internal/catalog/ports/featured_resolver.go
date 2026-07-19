package ports

import (
	"context"

	"altune/go-api/internal/catalog/domain"
)

// FeaturedArtistResolver resolves the featured ("feat.") artists for a track from
// the discovery providers. Implemented by a bridge adapter over the discovery
// FeaturedArtistResolver (never a direct catalog→discovery import). Consumed by
// the featured-artist backfill use case.
type FeaturedArtistResolver interface {
	Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error)
}

// NoopFeaturedArtistResolver returns empty credits for every track. Callers
// that don't wire a real resolver (e.g. tests) can default to this instead of
// guarding every Resolve call against a nil field.
func NoopFeaturedArtistResolver() FeaturedArtistResolver { return noopFeaturedArtistResolver{} }

type noopFeaturedArtistResolver struct{}

func (noopFeaturedArtistResolver) Resolve(_ context.Context, _, _ string) ([]domain.FeaturedArtist, error) {
	return nil, nil
}
