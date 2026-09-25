package app

import (
	"altune/go-api/internal/discovery/adapters/providers"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"

	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"
)

func (a *App) buildDetailEnrichers(cf clientFactory) discoveryHandler.DetailEnrichers {
	var enrichers discoveryHandler.DetailEnrichers

	if lfmEnricher := buildLastFMAdapter(a.cfg, cf.discovery()); lfmEnricher != nil {
		enrichers.LastFm = discoveryEnrich.NewLastFmEnrichmentService(
			lfmEnricher,
			discoveryCacheAdapters.NewRedisLastFmEnrichmentCache(a.redisClient, cacheSignalOption()),
		)
	}

	enrichers.Deezer = discoveryEnrich.NewDeezerEnrichmentService(
		providers.NewDeezerAdapter(cf.discovery()),
		discoveryCacheAdapters.NewRedisDeezerEnrichmentCache(a.redisClient, cacheSignalOption()),
	)

	enrichers.Lyrics = discoveryEnrich.NewLyricsService(
		providers.NewDeezerLyricsAdapter(cf.discovery()),
		discoveryCacheAdapters.NewRedisDeezerLyricsCache(a.redisClient, cacheSignalOption()),
	)

	return enrichers
}
