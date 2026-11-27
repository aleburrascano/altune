package setup

import (
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/service/artistpage"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/search/internal/service/related"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
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
	r0, err := artistpage.NewGetAlbumTracksService(t.albumProviders(), featured, fallback, breaker)
	if err != nil {
		return r0, fmt.Errorf("new get album tracks service: %w", err)
	}
	return r0, nil
}

func albumFeaturedEnrichment(deezer provider, breaker *providerguard.CircuitBreaker) (artistpage.FeaturedEnrichment, error) {
	if deezer.trackFeatured == nil {
		return artistpage.NoFeaturedEnrichment{}, nil
	}
	r0, err := artistpage.NewDeezerFeaturedEnrichment(deezer.trackFeatured, breaker)
	if err != nil {
		return r0, fmt.Errorf("new deezer featured enrichment: %w", err)
	}
	return r0, nil
}

func albumFallback(deezer provider, breaker *providerguard.CircuitBreaker) (artistpage.AlbumFallback, error) {
	if deezer.albumFallback == nil {
		return artistpage.NoAlbumFallback{}, nil
	}
	r0, err := artistpage.NewSearchAlbumFallback(deezer.albumFallback, breaker)
	if err != nil {
		return r0, fmt.Errorf("new search album fallback: %w", err)
	}
	return r0, nil
}

func newRelatedService(t *providerTable, breaker *providerguard.CircuitBreaker) (*related.GetRelatedTracksService, error) {
	r0, err := related.NewGetRelatedTracksService(t.relatedProviders(), breaker)
	if err != nil {
		return r0, fmt.Errorf("new get related tracks service: %w", err)
	}
	return r0, nil
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
	telemetry, err := artistpage.NewDiscographyTelemetry(s.Events, bg, clock.Now)
	if err != nil {
		return nil, fmt.Errorf("build the discography telemetry: %w", err)
	}
	r0, err := artistpage.NewGetArtistContentService(t.artistProviders(), identity, anchoring, telemetry, breaker)
	if err != nil {
		return r0, fmt.Errorf("new get artist content service: %w", err)
	}
	return r0, nil
}

func artistIdentity(s connections.Stores) (artistpage.ArtistIdentity, error) {
	if s.Identities == nil {
		return artistpage.SeedPathIdentity{}, nil
	}
	r0, err := artistpage.NewStoreIdentity(s.Identities)
	if err != nil {
		return r0, fmt.Errorf("new store identity: %w", err)
	}
	return r0, nil
}

func artistAnchoring(musicBrainz provider, breaker *providerguard.CircuitBreaker) (artistpage.Anchoring, error) {
	if musicBrainz.artistAnchor == nil {
		return artistpage.NoAnchoring{}, nil
	}
	r0, err := artistpage.NewMusicBrainzAnchoring(musicBrainz.artistAnchor, breaker)
	if err != nil {
		return r0, fmt.Errorf("new music brainz anchoring: %w", err)
	}
	return r0, nil
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
	r0, err := artistpage.NewFeaturedArtistResolver(mb, deezer)
	if err != nil {
		return r0, fmt.Errorf("new featured artist resolver: %w", err)
	}
	return r0, nil
}
