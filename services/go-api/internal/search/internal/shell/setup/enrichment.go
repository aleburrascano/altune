package setup

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"fmt"
)

func newEnrichment(t *providerTable, s connections.Stores, artwork ports.TaggingArtworkLookup) (handler.Enricher, error) {
	enricher := t.named(rowMusicBrainz).enrichment
	if enricher == nil {
		return handler.UnconfiguredEnrichment{}, nil
	}
	enrichmentCache := cache.NewRedisEnrichmentCache(s.Redis, s.CacheOption())
	r0, err := service.NewEnrichmentService(enricher, artwork, enrichmentCache, enrichmentCache)
	if err != nil {
		return r0, fmt.Errorf("new enrichment service: %w", err)
	}
	return r0, nil
}

func newDetailEnrichers(t *providerTable, s connections.Stores) (handler.DetailEnrichers, error) {
	deezer := t.named(rowDeezer)
	deezerEnrichment, err := service.NewDeezerEnrichmentService(deezer.deezerDetails, cache.NewRedisDeezerEnrichmentCache(s.Redis, s.CacheOption()))
	if err != nil {
		return handler.DetailEnrichers{}, fmt.Errorf("build the deezer enrichment service: %w", err)
	}
	lyrics, err := service.NewLyricsService(deezer.lyrics, cache.NewRedisDeezerLyricsCache(s.Redis, s.CacheOption()))
	if err != nil {
		return handler.DetailEnrichers{}, fmt.Errorf("build the lyrics service: %w", err)
	}
	return handler.DetailEnrichers{Deezer: deezerEnrichment, Lyrics: lyrics}, nil
}
