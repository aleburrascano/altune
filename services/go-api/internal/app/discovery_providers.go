package app

import (
	"altune/go-api/internal/discovery/adapters/providers"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"

	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"
)

func (a *App) buildDetailEnrichers() discoveryHandler.DetailEnrichers {
	var enrichers discoveryHandler.DetailEnrichers

	if a.cfg.HasLastFM() {
		lfmEnricher := providers.NewLastFmAdapter(newDiscoveryClient(), a.cfg.LastFMAPIKey)
		enrichers.LastFm = discoveryEnrich.NewLastFmEnrichmentService(
			lfmEnricher,
			discoveryCacheAdapters.NewRedisLastFmEnrichmentCache(a.redisClient),
		)
	}

	enrichers.Deezer = discoveryEnrich.NewDeezerEnrichmentService(
		providers.NewDeezerAdapter(newDiscoveryClient()),
		discoveryCacheAdapters.NewRedisDeezerEnrichmentCache(a.redisClient),
	)

	enrichers.Lyrics = discoveryEnrich.NewLyricsService(
		providers.NewDeezerLyricsAdapter(newDiscoveryClient()),
		discoveryCacheAdapters.NewRedisDeezerLyricsCache(a.redisClient),
	)

	return enrichers
}
