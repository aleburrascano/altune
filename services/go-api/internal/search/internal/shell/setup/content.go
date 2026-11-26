package setup

import (
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/service/artistpage"
	"altune/go-api/internal/search/service/providerguard"
	"altune/go-api/internal/search/service/related"
	"altune/go-api/internal/shared/background"
	"fmt"
)

func newAlbumService(t *providerTable, breaker *providerguard.CircuitBreaker) (*artistpage.GetAlbumTracksService, error) {
	deezer := t.named(rowDeezer)
	featured, err := albumFeaturedEnrichment(deezer, breaker)
	if err != nil {
		return nil, err
	}
	fallback, err := albumFallback(deezer, breaker)
	if err != nil {
		return nil, err
	}
	return artistpage.NewGetAlbumTracksService(t.albumProviders(), featured, fallback, breaker)
}

func albumFeaturedEnrichment(deezer provider, breaker *providerguard.CircuitBreaker) (artistpage.FeaturedEnrichment, error) {
	if deezer.trackFeatured == nil {
		return artistpage.NoFeaturedEnrichment{}, nil
	}
	return artistpage.NewDeezerFeaturedEnrichment(deezer.trackFeatured, breaker)
}

func albumFallback(deezer provider, breaker *providerguard.CircuitBreaker) (artistpage.AlbumFallback, error) {
	if deezer.albumFallback == nil {
		return artistpage.NoAlbumFallback{}, nil
	}
	return artistpage.NewSearchAlbumFallback(deezer.albumFallback, breaker)
}

func newRelatedService(t *providerTable, breaker *providerguard.CircuitBreaker) *related.GetRelatedTracksService {
	return related.NewGetRelatedTracksService(t.relatedProviders(), related.WithRelatedCircuitBreaker(breaker))
}

func newArtistService(t *providerTable, s connections.Stores, breaker *providerguard.CircuitBreaker, bg *background.Tracker) (*artistpage.GetArtistContentService, error) {
	identity, err := artistIdentity(s)
	if err != nil {
		return nil, err
	}
	anchoring, err := artistAnchoring(t.named(rowMusicBrainz), breaker)
	if err != nil {
		return nil, err
	}
	telemetry, err := artistpage.NewDiscographyTelemetry(s.Events, bg)
	if err != nil {
		return nil, fmt.Errorf("build the discography telemetry: %w", err)
	}
	return artistpage.NewGetArtistContentService(t.artistProviders(), identity, anchoring, telemetry, breaker)
}

func artistIdentity(s connections.Stores) (artistpage.ArtistIdentity, error) {
	if s.Identities == nil {
		return artistpage.SeedPathIdentity{}, nil
	}
	return artistpage.NewStoreIdentity(s.Identities)
}

func artistAnchoring(musicBrainz provider, breaker *providerguard.CircuitBreaker) (artistpage.Anchoring, error) {
	if musicBrainz.artistAnchor == nil {
		return artistpage.NoAnchoring{}, nil
	}
	return artistpage.NewMusicBrainzAnchoring(musicBrainz.artistAnchor, breaker)
}

func newFeaturedArtists(t *providerTable) (*artistpage.FeaturedArtistResolver, error) {
	var mb artistpage.MBFeaturedSearcher = artistpage.NoMBFeatured{}
	if searcher := t.named(rowMusicBrainz).featuredSearch; searcher != nil {
		mb = searcher
	}
	var deezer artistpage.DeezerFeaturedLookup = artistpage.NoDeezerFeatured{}
	if lookup := t.named(rowDeezer).trackFeatured; lookup != nil {
		deezer = lookup
	}
	return artistpage.NewFeaturedArtistResolver(mb, deezer)
}
