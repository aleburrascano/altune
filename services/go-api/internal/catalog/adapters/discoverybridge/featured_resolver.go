package discoverybridge

import (
	"altune/go-api/internal/shared"
	"context"
	"log/slog"

	catalogdomain "altune/go-api/internal/catalog/domain"
	catalogports "altune/go-api/internal/catalog/ports"
)

var _ catalogports.FeaturedArtistResolver = (*FeaturedResolver)(nil)

type featuredArtistResolver interface {
	Resolve(ctx context.Context, artist, title string) ([]shared.FeaturedArtist, error)
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
		fa, ok := catalogdomain.NewFeaturedArtist(f.Name, f.MBID, f.DeezerID)
		if !ok {
			continue
		}
		// Provider data is not trusted to fit the catalog's field caps: an
		// oversized credit is dropped so the rest of the track still backfills.
		if err := catalogdomain.ValidateFeaturedArtist(fa); err != nil {
			slog.WarnContext(ctx, "featured artist skipped: exceeds field cap",
				"name_length", len(fa.Name), "mbid_length", len(fa.MBID), "error", err)
			continue
		}
		out = append(out, fa)
	}
	return out, nil
}
