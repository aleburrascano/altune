package app

import (
	"log/slog"
	"net/http"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	"altune/go-api/internal/discovery/adapters/providers"
	domain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func BuildSearchService(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
) *discoveryService.Service {
	return BuildSearchServiceWithTransport(cfg, pool, redisClient, eventStore, nil, nil, false)
}

func BuildSearchServiceWithTransport(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
	transport http.RoundTripper,
	vocabStore discoveryPorts.VocabularyStore,
	rankingOnly bool,
) *discoveryService.Service {
	cf := clientFactory{transport: transport}

	sharedMB := buildMusicBrainzAdapter(cf, cfg)

	searchProviders := buildSearchProviderList(cf, cfg, sharedMB)
	circuitBreaker := discoveryService.NewCircuitBreaker()

	opts := baseSearchOptions(cfg, pool, rankingOnly)
	if !rankingOnly {
		opts = append(opts, contentSearchOptions(cf, cfg, pool, redisClient, sharedMB)...)
	}
	opts = append(opts, cacheSearchOptions(redisClient, rankingOnly)...)
	opts = append(opts, vocabularySearchOptions(redisClient, vocabStore)...)
	opts = append(opts, eventSearchOptions(cfg, eventStore)...)
	if sharedMB != nil {
		opts = append(opts, discoveryService.WithAlbumValidator(sharedMB))
	}

	return discoveryService.NewService(searchProviders, circuitBreaker, opts...)
}

// baseSearchOptions wires the ranking concerns present on every call site:
// history persistence plus the independently-toggleable tail-demotion,
// cross-kind-prominence and exploration ranking tweaks.
func baseSearchOptions(cfg *config.Config, pool *pgxpool.Pool, rankingOnly bool) []discoveryService.Option {
	opts := []discoveryService.Option{
		discoveryService.WithHistoryRepository(discoveryPersistence.NewPgxSearchHistoryRepository(pool)),
	}
	if cfg.TailDemotionEnabled {
		opts = append(opts, discoveryService.WithTailDemotion())
	}
	if cfg.CrossKindProminenceEnabled {
		opts = append(opts, discoveryService.WithCrossKindProminence())
	}
	if cfg.ExplorationEnabled && !rankingOnly {
		opts = append(opts, discoveryService.WithExploration(cfg.ExplorationRate))
	}
	return opts
}

// contentSearchOptions wires the content-serving concerns skipped in
// ranking-only mode: artwork resolution, related lookups, favorites, the
// identity store and (when configured) identity verification on persist.
func contentSearchOptions(
	cf clientFactory,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	sharedMB *providers.MusicBrainzAdapter,
) []discoveryService.Option {
	deezerContent := providers.NewDeezerAdapter(cf.discovery())
	relationshipQuerier := discoveryPersistence.NewPgxRelationshipQuerier(pool)
	findRelatedSvc := discoveryService.NewFindRelatedService(relationshipQuerier, deezerContent, deezerContent)
	opts := []discoveryService.Option{
		discoveryService.WithArtworkResolver(buildArtworkChain(cf, cfg)),
		discoveryService.WithFindRelatedService(findRelatedSvc),
	}
	if pool != nil {
		opts = append(opts, discoveryService.WithFavorites(
			discoveryPersistence.NewPgxFavoritesRepository(pool),
		))
		identityStore := discoveryCacheAdapters.NewRedisIdentityStore(
			discoveryPersistence.NewPgxIdentityStore(pool),
			redisClient,
		)
		opts = append(opts, discoveryService.WithIdentityStore(identityStore))
	}
	if cfg.IdentityVerifyOnPersist && sharedMB != nil {
		// Identity verification intentionally runs on a narrower set than the
		// canonical artist-content map: Deezer + Spotify + Apple Music only.
		// Build the shared map, then drop SoundCloud and Last.fm so this set is
		// preserved exactly. Whether the verifier SHOULD include them is a
		// behavior question tracked separately (see PR), not decided here.
		verifyProviders := buildArtistContentProviders(cf, cfg)
		delete(verifyProviders, domain.ProviderSoundCloud)
		delete(verifyProviders, domain.ProviderLastFM)
		opts = append(opts, discoveryService.WithIdentityVerifier(
			discoveryService.NewIdentityVerifier(sharedMB, verifyProviders),
		))
	}
	return opts
}

// cacheSearchOptions wires the Redis-backed caches. The result cache is
// content-only; artwork cache, identity bridge and MBID index apply even in
// ranking-only mode.
func cacheSearchOptions(redisClient *goredis.Client, rankingOnly bool) []discoveryService.Option {
	if redisClient == nil {
		return nil
	}
	var opts []discoveryService.Option
	if !rankingOnly {
		opts = append(opts, discoveryService.WithResultCache(
			discoveryCacheAdapters.NewRedisResultCache(redisClient),
		))
	}
	opts = append(opts, discoveryService.WithArtworkCache(
		discoveryCacheAdapters.NewRedisArtworkCache(redisClient),
	))
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(redisClient)
	opts = append(opts,
		discoveryService.WithIdentityBridge(enrichmentCache),
		discoveryService.WithMBIDIndex(enrichmentCache),
	)
	return opts
}

// vocabularySearchOptions wires the vocabulary store, falling back to a
// Redis-backed store when the caller does not supply one.
func vocabularySearchOptions(redisClient *goredis.Client, vocabStore discoveryPorts.VocabularyStore) []discoveryService.Option {
	vs := vocabStore
	if vs == nil {
		vs = BuildVocabularyStore(redisClient)
	}
	if vs == nil {
		return nil
	}
	return []discoveryService.Option{discoveryService.WithVocabularyStore(vs)}
}

// eventSearchOptions wires the event store and, when behavioral ranking is
// enabled and the store supports it, the satisfaction-signal consumer.
func eventSearchOptions(cfg *config.Config, eventStore discoveryPorts.EventStore) []discoveryService.Option {
	if eventStore == nil {
		return nil
	}
	opts := []discoveryService.Option{discoveryService.WithEventStore(eventStore)}
	if cfg.BehavioralRankingEnabled {
		if store, ok := eventStore.(discoveryPorts.BehavioralSignalStore); ok {
			opts = append(opts, discoveryService.WithBehavioralRanking(
				discoveryService.NewSatisfactionConsumer(store),
			))
		}
	}
	return opts
}

func BuildDiscoveryProviders(cfg *config.Config, transport http.RoundTripper) []discoveryPorts.SearchProvider {
	cf := clientFactory{transport: transport}
	mb := buildMusicBrainzAdapter(cf, cfg)
	return buildSearchProviderList(cf, cfg, mb)
}

func buildSearchProviderList(cf clientFactory, cfg *config.Config, mb *providers.MusicBrainzAdapter) []discoveryPorts.SearchProvider {
	var providerList []discoveryPorts.SearchProvider

	deezerClient := cf.discovery()
	providerList = append(providerList, providers.NewDeezerAdapter(deezerClient))

	appleMusicClient := cf.discovery()
	providerList = append(providerList, providers.NewAppleMusicAdapter(appleMusicClient))

	if mb != nil {
		providerList = append(providerList, mb)
	}

	if cfg.HasLastFM() {
		lfmClient := cf.discovery()
		providerList = append(providerList, providers.NewLastFmAdapter(lfmClient, cfg.LastFMAPIKey))
	}

	soundcloudClient := cf.discovery()
	providerList = append(providerList,
		providers.NewSoundCloudAPIAdapter(
			soundcloudClient,
			providers.NewSoundCloudAdapter(),
		),
		providers.NewYouTubeMusicAdapter(cf.roundTripper()),
	)

	amazonClient := cf.discovery()
	providerList = append(providerList, providers.NewAmazonMusicAdapter(amazonClient))

	spotifyClient := cf.discovery()
	providerList = append(providerList, providers.NewSpotifyAdapter(spotifyClient))

	slog.Info("discovery providers configured", "count", len(providerList))
	return providerList
}

// buildMusicBrainzAdapter constructs the shared MusicBrainz adapter from the
// given client factory, returning nil when MusicBrainz is not configured. It is
// the single construction site for the adapter across the app wiring.
func buildMusicBrainzAdapter(cf clientFactory, cfg *config.Config) *providers.MusicBrainzAdapter {
	if !cfg.HasMusicBrainz() {
		return nil
	}
	return providers.NewMusicBrainzAdapter(cf.discovery(), cfg.MusicBrainzUserAgent)
}
