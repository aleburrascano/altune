package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

type mbFeaturedSearcher interface {
	SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

type deezerFeaturedLookup interface {
	ResolveID(ctx context.Context, kind domain.ResultKind, artist, title string) (string, error)
	LookupTrackFeatured(ctx context.Context, trackID string) ([]domain.FeaturedArtist, error)
}

type FeaturedArtistResolver struct {
	mb     mbFeaturedSearcher
	deezer deezerFeaturedLookup
}

func NewFeaturedArtistResolver(mb mbFeaturedSearcher, deezer deezerFeaturedLookup) *FeaturedArtistResolver {
	return &FeaturedArtistResolver{mb: mb, deezer: deezer}
}

func (r *FeaturedArtistResolver) Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error) {
	return MergeFeaturedArtists(r.mbFeatured(ctx, artist, title), r.deezerFeatured(ctx, artist, title)), nil
}

func (r *FeaturedArtistResolver) mbFeatured(ctx context.Context, artist, title string) []domain.FeaturedArtist {
	if r.mb == nil {
		return nil
	}
	results, err := r.mb.SearchStructured(ctx, artist, title, map[domain.ResultKind]bool{domain.ResultKindTrack: true})
	if err != nil {
		return nil
	}
	for _, res := range results {
		if res.Kind != domain.ResultKindTrack {
			continue
		}
		if feats := domain.FeaturedArtistsFromExtras(res.Extras); feats != nil {
			return feats
		}
	}
	return nil
}

func (r *FeaturedArtistResolver) deezerFeatured(ctx context.Context, artist, title string) []domain.FeaturedArtist {
	if r.deezer == nil {
		return nil
	}
	id, err := r.deezer.ResolveID(ctx, domain.ResultKindTrack, artist, title)
	if err != nil || id == "" {
		return nil
	}
	feats, err := r.deezer.LookupTrackFeatured(ctx, id)
	if err != nil {
		return nil
	}
	return feats
}
