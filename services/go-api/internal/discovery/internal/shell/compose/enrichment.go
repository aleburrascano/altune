package compose

import (
	"altune/go-api/internal/discovery/adapters/handler"
	"altune/go-api/internal/discovery/internal/adapters/cache"
	"altune/go-api/internal/discovery/internal/adapters/handles"
	"altune/go-api/internal/discovery/internal/core/enrich"
	"altune/go-api/internal/discovery/internal/core/ports"
)

func newEnrichmentService(t *providerTable, s handles.Stores, artwork ports.TaggingArtworkResolver) *enrich.EnrichmentService {
	enricher := t.named(rowMusicBrainz).enrichment
	if enricher == nil {
		return nil
	}
	enrichmentCache := cache.NewRedisEnrichmentCache(s.Redis, s.CacheOption())
	return enrich.NewEnrichmentService(enricher, artwork, enrichmentCache, enrich.WithMBIDMemo(enrichmentCache))
}

func newDetailEnrichers(t *providerTable, s handles.Stores) handler.DetailEnrichers {
	deezer := t.named(rowDeezer)
	return handler.DetailEnrichers{
		Deezer: enrich.NewDeezerEnrichmentService(deezer.deezerDetails, cache.NewRedisDeezerEnrichmentCache(s.Redis, s.CacheOption())),
		Lyrics: enrich.NewLyricsService(deezer.lyrics, cache.NewRedisDeezerLyricsCache(s.Redis, s.CacheOption())),
	}
}
