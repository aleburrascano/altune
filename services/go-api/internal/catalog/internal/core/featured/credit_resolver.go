package featured

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/internal/core/ports"
	"context"
	"log/slog"
)

type creditResolver struct {
	credits ports.FeaturedArtistCreditSource
}

func NewFeaturedArtistResolver(credits ports.FeaturedArtistCreditSource) ports.FeaturedArtistResolver {
	return creditResolver{credits: credits}
}

func (r creditResolver) Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error) {
	credits, err := r.credits.FeaturedArtistCredits(ctx, artist, title)
	if err != nil {
		return nil, err
	}
	out := make([]domain.FeaturedArtist, 0, len(credits))
	for _, credit := range credits {
		if fa, ok := storableFeaturedArtist(ctx, credit); ok {
			out = append(out, fa)
		}
	}
	return out, nil
}

func storableFeaturedArtist(ctx context.Context, credit ports.FeaturedArtistCredit) (domain.FeaturedArtist, bool) {
	fa, ok := domain.NewFeaturedArtist(credit.Name, credit.MBID, credit.DeezerID)
	if !ok {
		return domain.FeaturedArtist{}, false
	}
	if err := domain.ValidateFeaturedArtist(fa); err != nil {
		slog.WarnContext(ctx, "featured artist skipped: exceeds field cap",
			"name_length", len(fa.Name), "mbid_length", len(fa.MBID), "error", err)
		return domain.FeaturedArtist{}, false
	}
	return fa, true
}
