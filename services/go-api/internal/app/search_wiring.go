package app

import (
	"net/http"
	"time"

	catalogPersistence "altune/go-api/internal/catalog/adapters/persistence"
	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	"altune/go-api/internal/discovery/adapters/providers"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// BuildSearchService constructs the discovery search pipeline exactly as the API
// server wires it, so offline tooling (the library eval) exercises the same
// ranking the user sees. The single construction site keeps the eval from
// drifting away from production as wiring changes.
//
// eventStore is injected (not built here) so the server can share its telemetry
// store while the eval passes nil — eval searches are synthetic and must not
// pollute telemetry. redisClient may be nil (cache/vocab options are skipped).
func BuildSearchService(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
) *discoveryService.SearchMusicService {
	var sharedMB *providers.MusicBrainzAdapter
	if cfg.HasMusicBrainz() {
		sharedMB = providers.NewMusicBrainzAdapter(
			&http.Client{Timeout: 10 * time.Second},
			cfg.MusicBrainzUserAgent,
		)
	}

	searchProviders := buildDiscoveryProviders(cfg, sharedMB)
	queryCache := discoveryCacheAdapters.NewRedisQueryCache(redisClient)
	circuitBreaker := discoveryService.NewCircuitBreaker()
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(pool)
	clickRepo := discoveryPersistence.NewPgxSearchClickRepository(pool)

	searchOpts := []discoveryService.SearchOption{
		discoveryService.WithArtworkResolver(buildArtworkChain(cfg)),
	}
	if redisClient != nil {
		searchOpts = append(searchOpts, discoveryService.WithArtworkCache(
			discoveryCacheAdapters.NewRedisArtworkCache(redisClient),
		))
	}

	if vocabStore := buildVocabularyStore(redisClient); vocabStore != nil {
		searchOpts = append(searchOpts, discoveryService.WithVocabularyStore(vocabStore))
	}

	searchOpts = append(searchOpts, discoveryService.WithClickSignals(clickRepo))
	if eventStore != nil {
		searchOpts = append(searchOpts, discoveryService.WithEventStore(eventStore))
	}
	if sharedMB != nil {
		searchOpts = append(searchOpts, discoveryService.WithIdentityResolver(sharedMB))
	}

	deezerContent := providers.NewDeezerAdapter(&http.Client{Timeout: 10 * time.Second})
	trackRepo := catalogPersistence.NewPgxTrackRepository(pool)
	findRelatedSvc := discoveryService.NewFindRelatedService(trackRepo, deezerContent, deezerContent)
	searchOpts = append(searchOpts, discoveryService.WithFindRelatedService(findRelatedSvc))

	return discoveryService.NewSearchMusicService(searchProviders, queryCache, historyRepo, circuitBreaker, searchOpts...)
}

// buildArtworkChain assembles the artwork resolver chain: ID-based sources first
// (always correct for the entity), name-search fallbacks last.
func buildArtworkChain(cfg *config.Config) discoveryPorts.ArtworkResolver {
	var artworkResolvers []discoveryPorts.ArtworkResolver
	artworkResolvers = append(artworkResolvers,
		providers.NewCoverArtArchiveResolver(&http.Client{Timeout: 10 * time.Second}))
	if cfg.HasFanartTV() {
		artworkResolvers = append(artworkResolvers,
			providers.NewFanartTvArtworkResolver(&http.Client{Timeout: 10 * time.Second}, cfg.FanartTVAPIKey))
	}
	if cfg.HasGenius() {
		artworkResolvers = append(artworkResolvers,
			providers.NewGeniusArtworkResolver(&http.Client{Timeout: 10 * time.Second}, cfg.GeniusAccessToken))
	}
	artworkResolvers = append(artworkResolvers,
		providers.NewTheAudioDBAdapter(&http.Client{Timeout: 10 * time.Second}),
		providers.NewDeezerAdapter(&http.Client{Timeout: 10 * time.Second}),
		providers.NewITunesAdapter(&http.Client{Timeout: 10 * time.Second}),
	)
	if cfg.HasYouTube() {
		artworkResolvers = append(artworkResolvers,
			providers.NewYouTubeArtworkResolver(&http.Client{Timeout: 10 * time.Second}, cfg.YouTubeAPIKey))
	}
	return providers.NewChainedArtworkResolver(artworkResolvers...)
}

// buildVocabularyStore returns the Redis-backed vocabulary store, or nil when
// Redis is not configured.
func buildVocabularyStore(redisClient *goredis.Client) discoveryPorts.VocabularyStore {
	if redisClient == nil {
		return nil
	}
	return discoveryCacheAdapters.NewVocabularyStore(
		redisClient,
		discoveryService.NormalizeForMatch,
		discoveryCacheAdapters.WithMetaphone(discoveryService.MetaphoneKey),
	)
}
