package setup

import (
	"altune/go-api/internal/search/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/core/details"
	"altune/go-api/internal/search/internal/core/ports"
	"errors"
)

var errEnrichmentSwitchedOff = errors.New("enrichment is switched off: MusicBrainz is not configured")

func newEnrichmentService(t *providerTable, s connections.Stores, artwork ports.TaggingArtworkLookup) (*details.DetailsService, error) {
	enricher := t.named(rowMusicBrainz).enrichment
	if enricher == nil {
		return nil, errEnrichmentSwitchedOff
	}
	enrichmentCache := cache.NewRedisEnrichmentCache(s.Redis, s.CacheOption())
	return details.NewEnrichmentService(enricher, artwork, enrichmentCache, enrichmentCache)
}

func newDetailEnrichers(t *providerTable, s connections.Stores) handler.DetailEnrichers {
	deezer := t.named(rowDeezer)
	return handler.DetailEnrichers{
		Deezer: details.NewDeezerEnrichmentService(deezer.deezerDetails, cache.NewRedisDeezerEnrichmentCache(s.Redis, s.CacheOption())),
		Lyrics: details.NewLyricsService(deezer.lyrics, cache.NewRedisDeezerLyricsCache(s.Redis, s.CacheOption())),
	}
}
