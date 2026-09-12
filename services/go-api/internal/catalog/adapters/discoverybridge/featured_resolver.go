package discoverybridge

import (
	"context"

	catalogdomain "altune/go-api/internal/catalog/domain"
	catalogports "altune/go-api/internal/catalog/ports"
	discoverydomain "altune/go-api/internal/discovery/domain"
)

var _ catalogports.FeaturedArtistResolver = (*FeaturedResolver)(nil)

type featuredArtistResolver interface {
	Resolve(ctx context.Context, artist, title string) ([]discoverydomain.FeaturedArtist, error)
}

type FeaturedResolver struct {
	inner featuredArtistResolver
}

func NewFeaturedResolver(inner featuredArtistResolver) *FeaturedResolver {
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
