package libraryfromsearch

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/search"
	"context"
	"fmt"
)

type FeaturedArtistSource interface {
	Resolve(ctx context.Context, artist, title string) ([]search.FeaturedArtist, error)
}

type featuredArtistCredits struct {
	source FeaturedArtistSource
}

func NewFeaturedArtistCredits(source FeaturedArtistSource) library.FeaturedArtistCreditSource {
	return featuredArtistCredits{source: source}
}

func (c featuredArtistCredits) FeaturedArtistCredits(ctx context.Context, artist, title string) ([]library.FeaturedArtistCredit, error) {
	feats, err := c.source.Resolve(ctx, artist, title)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	credits := make([]library.FeaturedArtistCredit, len(feats))
	for i, f := range feats {
		credits[i] = library.FeaturedArtistCredit{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID}
	}
	return credits, nil
}
