package app

import (
	"altune/go-api/internal/discovery/adapters/providers"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"

	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"
)

func (a *App) buildDetailEnrichers(cf clientFactory) discoveryHandler.DetailEnrichers {
	var enrichers discoveryHandler.DetailEnrichers

	if a.cfg.HasLastFM() {
		lfmEnricher := providers.NewLastFmAdapter(cf.discovery(), a.cfg.LastFMAPIKey)
		enrichers.LastFm = discoveryEnrich.NewLastFmEnrichmentService(
			lfmEnricher,
			discoveryCacheAdapters.NewRedisLastFmEnrichmentCache(a.redisClient),
		)
	}

	enrichers.Deezer = discoveryEnrich.NewDeezerEnrichmentService(
		providers.NewDeezerAdapter(cf.discovery()),
		discoveryCacheAdapters.NewRedisDeezerEnrichmentCache(a.redisClient),
	)

	enrichers.Lyrics = discoveryEnrich.NewLyricsService(
		providers.NewDeezerLyricsAdapter(cf.discovery()),
		discoveryCacheAdapters.NewRedisDeezerLyricsCache(a.redisClient),
	)

	return enrichers
}
