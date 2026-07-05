package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

// mbFeaturedSearcher is the MusicBrainz surface the resolver needs: a structured
// (artist, track) search whose recording results carry featured artists in
// Extras["featured_artists"] (populated by mapMBRecording).
type mbFeaturedSearcher interface {
	SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

// deezerFeaturedLookup is the Deezer surface: resolve a (kind, artist, title) to a
// Deezer track id, then fetch that track's featured contributors.
type deezerFeaturedLookup interface {
	ResolveID(ctx context.Context, kind domain.ResultKind, artist, title string) (string, error)
	LookupTrackFeatured(ctx context.Context, trackID string) ([]domain.FeaturedArtist, error)
}

// FeaturedArtistResolver resolves the featured artists for a track from the
// discovery providers, MB-primary with Deezer filling gaps. Reused by the catalog
// backfill (via a bridge) to populate featured artists for existing tracks. A
// provider failure degrades to that provider contributing nothing rather than
// failing the whole resolve.
type FeaturedArtistResolver struct {
	mb     mbFeaturedSearcher
	deezer deezerFeaturedLookup
}

func NewFeaturedArtistResolver(mb mbFeaturedSearcher, deezer deezerFeaturedLookup) *FeaturedArtistResolver {
	return &FeaturedArtistResolver{mb: mb, deezer: deezer}
}

// Resolve returns the merged featured artists for (artist, title). Either
// provider may be absent (nil) — then only the other contributes.
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
		raw, ok := res.Extras["featured_artists"].([]map[string]any)
		if !ok {
			continue
		}
		out := make([]domain.FeaturedArtist, 0, len(raw))
		for _, m := range raw {
			out = append(out, domain.FeaturedArtistFromMap(m))
		}
		return out
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
