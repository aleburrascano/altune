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
