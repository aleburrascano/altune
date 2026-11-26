package artistpage

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/service/providerguard"
	"altune/go-api/internal/shared/parallel"
	"context"
)

const albumFeaturedConcurrency = 5

type FeaturedEnrichment interface {
	enrich(ctx context.Context, results []domain.SearchResult, answered map[int]bool)
}

type NoFeaturedEnrichment struct{}

func (NoFeaturedEnrichment) enrich(context.Context, []domain.SearchResult, map[int]bool) {}

type DeezerFeaturedEnrichment struct {
	lookup  DeezerFeaturedLookup
	breaker *providerguard.CircuitBreaker
}

func NewDeezerFeaturedEnrichment(lookup DeezerFeaturedLookup, breaker *providerguard.CircuitBreaker) (*DeezerFeaturedEnrichment, error) {
	if err := requireCollaborators("the Deezer featured enrichment",
		collaborator{"a Deezer featured lookup", lookup == nil},
		collaborator{"a circuit breaker", breaker == nil},
	); err != nil {
		return nil, err
	}
	return &DeezerFeaturedEnrichment{lookup: lookup, breaker: breaker}, nil
}

func (e *DeezerFeaturedEnrichment) enrich(ctx context.Context, results []domain.SearchResult, answered map[int]bool) {
	g := parallel.New("album_tracks.featured_panic", albumFeaturedConcurrency)
	for i := range results {
		if !answered[i] && hasDeezerTrackID(results[i]) {
			g.Go(ctx, func() {
				e.lookupFeatured(ctx, &results[i])
			}, "external_id", results[i].Sources[0].ExternalID)
		}
	}
	_ = g.Wait()
}

func hasDeezerTrackID(r domain.SearchResult) bool {
	return r.Kind == domain.ResultKindTrack && len(r.Sources) > 0 &&
		domain.IsCanonicalContentProvider(r.Sources[0].Provider) && r.Sources[0].ExternalID != ""
}

func (e *DeezerFeaturedEnrichment) lookupFeatured(ctx context.Context, track *domain.SearchResult) {
	trackID := track.Sources[0].ExternalID
	feats, err := providerguard.GuardedFetch(ctx, e.breaker, domain.CanonicalContentProvider, func() ([]domain.FeaturedArtist, error) {
		return e.lookup.LookupTrackFeatured(ctx, trackID)
	})
	if err == nil && len(feats) > 0 {
		*track = track.WithExtra(domain.ExtraFeaturedArtists, domain.FeaturedArtistsToExtras(feats))
	}
}
