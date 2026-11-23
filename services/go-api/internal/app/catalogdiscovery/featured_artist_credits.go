package catalogdiscovery

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"context"
)

type FeaturedArtistSource interface {
	Resolve(ctx context.Context, artist, title string) ([]discovery.FeaturedArtist, error)
}

type featuredArtistCredits struct {
	source FeaturedArtistSource
}

func NewFeaturedArtistCredits(source FeaturedArtistSource) catalog.FeaturedArtistCreditSource {
	return featuredArtistCredits{source: source}
}

func (c featuredArtistCredits) FeaturedArtistCredits(ctx context.Context, artist, title string) ([]catalog.FeaturedArtistCredit, error) {
	feats, err := c.source.Resolve(ctx, artist, title)
	if err != nil {
		return nil, err
	}
	credits := make([]catalog.FeaturedArtistCredit, len(feats))
	for i, f := range feats {
		credits[i] = catalog.FeaturedArtistCredit{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID}
	}
	return credits, nil
}
