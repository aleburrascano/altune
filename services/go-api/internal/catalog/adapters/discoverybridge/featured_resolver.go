// Package discoverybridge adapts discovery-context services to catalog ports,
// keeping catalog free of a direct discovery import (mirrors playback/catalogbridge).
package discoverybridge

import (
	"context"

	catalogdomain "altune/go-api/internal/catalog/domain"
	catalogports "altune/go-api/internal/catalog/ports"
	discoveryservice "altune/go-api/internal/discovery/service"
)

var _ catalogports.FeaturedArtistResolver = (*FeaturedResolver)(nil)

// FeaturedResolver satisfies the catalog FeaturedArtistResolver port by delegating
// to the discovery resolver and translating discovery value objects into catalog
// ones.
type FeaturedResolver struct {
	inner *discoveryservice.FeaturedArtistResolver
}

func NewFeaturedResolver(inner *discoveryservice.FeaturedArtistResolver) *FeaturedResolver {
	return &FeaturedResolver{inner: inner}
}

func (r *FeaturedResolver) Resolve(ctx context.Context, artist, title string) ([]catalogdomain.FeaturedArtist, error) {
	feats, err := r.inner.Resolve(ctx, artist, title)
	if err != nil {
		return nil, err
	}
	out := make([]catalogdomain.FeaturedArtist, 0, len(feats))
	for _, f := range feats {
		if fa, ok := catalogdomain.NewFeaturedArtist(f.Name, f.MBID, f.DeezerID); ok {
			out = append(out, fa)
		}
	}
	return out, nil
}
