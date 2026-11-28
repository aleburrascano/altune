package setup

import (
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"fmt"
)

func newAlbumService(t *providerTable, breaker *service.CircuitBreaker) (*service.GetAlbumTracksService, error) {
	deezer := t.named(rowDeezer)
	featured, err := albumFeaturedEnrichment(deezer, breaker)
	if err != nil {
		return nil, err
	}
	fallback, err := albumFallback(deezer, breaker)
	if err != nil {
		return nil, err
	}
	r0, err := service.NewGetAlbumTracksService(t.albumProviders(), featured, fallback, breaker)
	if err != nil {
		return r0, fmt.Errorf("new get album tracks service: %w", err)
	}
	return r0, nil
}

func albumFeaturedEnrichment(deezer provider, breaker *service.CircuitBreaker) (service.FeaturedEnrichment, error) {
	if deezer.trackFeatured == nil {
		return service.NoFeaturedEnrichment{}, nil
	}
	r0, err := service.NewDeezerFeaturedEnrichment(deezer.trackFeatured, breaker)
	if err != nil {
		return r0, fmt.Errorf("new deezer featured enrichment: %w", err)
	}
	return r0, nil
}

func albumFallback(deezer provider, breaker *service.CircuitBreaker) (service.AlbumFallback, error) {
	if deezer.albumFallback == nil {
		return service.NoAlbumFallback{}, nil
	}
	r0, err := service.NewSearchAlbumFallback(deezer.albumFallback, breaker)
	if err != nil {
		return r0, fmt.Errorf("new search album fallback: %w", err)
	}
	return r0, nil
}

func newRelatedService(t *providerTable, breaker *service.CircuitBreaker) (*service.GetRelatedTracksService, error) {
	r0, err := service.NewGetRelatedTracksService(t.relatedProviders(), breaker)
	if err != nil {
		return r0, fmt.Errorf("new get related tracks service: %w", err)
	}
	return r0, nil
}

func newArtistService(t *providerTable, s connections.Stores, breaker *service.CircuitBreaker, bg *background.Tracker) (*service.GetArtistContentService, error) {
	identity, err := artistIdentity(s)
	if err != nil {
		return nil, err
	}
	anchoring, err := artistAnchoring(t.named(rowMusicBrainz), breaker)
	if err != nil {
		return nil, err
	}
	telemetry, err := service.NewDiscographyTelemetry(s.Events, bg, clock.Now)
	if err != nil {
		return nil, fmt.Errorf("build the discography telemetry: %w", err)
	}
	r0, err := service.NewGetArtistContentService(t.artistProviders(), t.albumProviders(), identity, anchoring, telemetry, breaker)
	if err != nil {
		return r0, fmt.Errorf("new get artist content service: %w", err)
	}
	return r0, nil
}

func artistIdentity(s connections.Stores) (service.ArtistIdentity, error) {
	if s.Identities == nil {
		return service.SeedPathIdentity{}, nil
	}
	r0, err := service.NewStoreIdentity(s.Identities)
	if err != nil {
		return r0, fmt.Errorf("new store identity: %w", err)
	}
	return r0, nil
}

func artistAnchoring(musicBrainz provider, breaker *service.CircuitBreaker) (service.Anchoring, error) {
	if musicBrainz.artistAnchor == nil {
		return service.NoAnchoring{}, nil
	}
	r0, err := service.NewMusicBrainzAnchoring(musicBrainz.artistAnchor, breaker)
	if err != nil {
		return r0, fmt.Errorf("new music brainz anchoring: %w", err)
	}
	return r0, nil
}

func newFeaturedArtists(t *providerTable) (*service.FeaturedArtistResolver, error) {
	var mb service.MBFeaturedSearcher = service.NoMBFeatured{}
	if searcher := t.named(rowMusicBrainz).featuredSearch; searcher != nil {
		mb = searcher
	}
	var deezer service.DeezerFeaturedLookup = service.NoDeezerFeatured{}
	if lookup := t.named(rowDeezer).trackFeatured; lookup != nil {
		deezer = lookup
	}
	r0, err := service.NewFeaturedArtistResolver(mb, deezer)
	if err != nil {
		return r0, fmt.Errorf("new featured artist resolver: %w", err)
	}
	return r0, nil
}
