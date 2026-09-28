package app

import (
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"net/http"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	domain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func BuildSearchService(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
) *discoveryService.Service {
	return BuildSearchServiceWithTransport(cfg, pool, redisClient, eventStore, nil, nil)
}

func BuildSearchServiceWithTransport(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
	transport http.RoundTripper,
	vocabStore discoveryPorts.VocabularyStore,
	extra ...discoveryService.Option,
) *discoveryService.Service {
	w := newSearchWiring(cfg, transport)
	opts := contentServiceOptions(w, cfg, pool, redisClient, eventStore, vocabStore)
	return w.service(append(opts, extra...))
}

func BuildRankingOnlySearchService(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	transport http.RoundTripper,
) *discoveryService.Service {
	w := newSearchWiring(cfg, transport)
	return w.service(rankingOnlyServiceOptions(w, cfg, pool, redisClient))
}

type searchWiring struct {
	cf        clientFactory
	sharedMB  *providers.MusicBrainzAdapter
	providers []discoveryPorts.SearchProvider
	breaker   *discoveryService.CircuitBreaker
}

func newSearchWiring(cfg *config.Config, transport http.RoundTripper) searchWiring {
	cf := newClientFactory(transport)
	sharedMB := buildMusicBrainzAdapter(cf, cfg)
	return searchWiring{
		cf:        cf,
		sharedMB:  sharedMB,
		providers: buildSearchProviderList(cf, cfg, sharedMB),
		breaker:   discoveryService.NewCircuitBreaker(),
	}
}

func (w searchWiring) service(opts []discoveryService.Option) *discoveryService.Service {
	return discoveryService.NewService(w.providers, w.breaker, opts...)
}

func contentServiceOptions(
	w searchWiring,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
	vocabStore discoveryPorts.VocabularyStore,
) []discoveryService.Option {
	opts := baseSearchOptions(cfg, pool)
	opts = append(opts, explorationSearchOptions(cfg)...)
	opts = append(opts, contentSearchOptions(w.cf, cfg, pool, redisClient, w.sharedMB)...)
	opts = append(opts, resultCacheSearchOptions(redisClient)...)
	return append(opts, sharedSearchOptions(w, cfg, redisClient, eventStore, vocabStore)...)
}

func rankingOnlyServiceOptions(
	w searchWiring,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
) []discoveryService.Option {
	opts := baseSearchOptions(cfg, pool)
	return append(opts, sharedSearchOptions(w, cfg, redisClient, nil, nil)...)
}

func sharedSearchOptions(
	w searchWiring,
	cfg *config.Config,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
	vocabStore discoveryPorts.VocabularyStore,
) []discoveryService.Option {
	opts := cacheSearchOptions(redisClient)
	opts = append(opts, vocabularySearchOptions(redisClient, vocabStore)...)
	opts = append(opts, eventSearchOptions(cfg, eventStore)...)
	if w.sharedMB != nil {
		opts = append(opts, discoveryService.WithAlbumValidator(w.sharedMB))
	}
	return opts
}

func baseSearchOptions(cfg *config.Config, pool *pgxpool.Pool) []discoveryService.Option {
	opts := []discoveryService.Option{
		discoveryService.WithHistoryRepository(discoveryPersistence.NewPgxSearchHistoryRepository(pool)),
	}
	if cfg.TailDemotionEnabled {
		opts = append(opts, discoveryService.WithTailDemotion())
	}
	if cfg.CrossKindProminenceEnabled {
		opts = append(opts, discoveryService.WithCrossKindProminence())
	}
	return opts
}

func explorationSearchOptions(cfg *config.Config) []discoveryService.Option {
	if !cfg.ExplorationEnabled {
		return nil
	}
	return []discoveryService.Option{discoveryService.WithExploration(cfg.ExplorationRate)}
}

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
			cacheSignalOption(),
		)
		opts = append(opts, discoveryService.WithIdentityStore(identityStore))
	}
	if cfg.IdentityVerifyOnPersist && sharedMB != nil {
		verifyProviders := buildArtistContentProviders(cf, cfg)
		delete(verifyProviders, domain.ProviderSoundCloud)
		delete(verifyProviders, domain.ProviderLastFM)
		opts = append(opts, discoveryService.WithIdentityVerifier(
			discoveryService.NewIdentityVerifier(sharedMB, verifyProviders),
		))
	}
	return opts
}

func resultCacheSearchOptions(redisClient *goredis.Client) []discoveryService.Option {
	if redisClient == nil {
		return nil
	}
	return []discoveryService.Option{
		discoveryService.WithResultCache(
			discoveryCacheAdapters.NewRedisResultCache(redisClient, cacheSignalOption()),
		),
		discoveryService.WithHeldSlateCache(
			discoveryCacheAdapters.NewRedisHeldSlateCache(redisClient, cacheSignalOption()),
		),
	}
}

func cacheSearchOptions(redisClient *goredis.Client) []discoveryService.Option {
	if redisClient == nil {
		return nil
	}
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(redisClient, cacheSignalOption())
	return []discoveryService.Option{
		discoveryService.WithArtworkCache(discoveryCacheAdapters.NewRedisArtworkCache(redisClient, cacheSignalOption())),
		discoveryService.WithIdentityBridge(enrichmentCache),
		discoveryService.WithMBIDIndex(enrichmentCache),
	}
}

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
	cf := newClientFactory(transport)
	mb := buildMusicBrainzAdapter(cf, cfg)
	return buildSearchProviderList(cf, cfg, mb)
}

func buildSearchProviderList(cf clientFactory, cfg *config.Config, mb *providers.MusicBrainzAdapter) []discoveryPorts.SearchProvider {
	var providerList []discoveryPorts.SearchProvider

	deezerClient := cf.discovery()
	providerList = append(providerList, providers.NewDeezerAdapter(deezerClient))

	if am := buildAppleMusicAdapter(cf, cfg); am != nil {
		providerList = append(providerList, am)
	}

	if mb != nil {
		providerList = append(providerList, mb)
	}

	if lfm := buildLastFMAdapter(cfg, cf.discovery()); lfm != nil {
		providerList = append(providerList, lfm)
	}

	providerList = append(providerList, buildScrapedSearchProviders(cf, cfg)...)

	slog.Info("discovery providers configured", "count", len(providerList))
	return providerList
}

func buildScrapedSearchProviders(cf clientFactory, cfg *config.Config) []discoveryPorts.SearchProvider {
	var list []discoveryPorts.SearchProvider
	if sc := buildSoundCloudSearchAdapter(cf, cfg); sc != nil {
		list = append(list, sc)
	}
	if cfg.HasYouTubeMusic() {
		list = append(list, providers.NewYouTubeMusicAdapter(cf.roundTripper()))
	}
	if cfg.HasAmazonMusic() {
		list = append(list, providers.NewAmazonMusicAdapter(cf.discovery()))
	}
	if sp := buildSpotifyAdapter(cf, cfg); sp != nil {
		list = append(list, sp)
	}
	return list
}

func buildLastFMAdapter(cfg *config.Config, client *http.Client) *providers.LastFmAdapter {
	if !cfg.HasLastFM() {
		return nil
	}
	return providers.NewLastFmAdapter(client, cfg.LastFMAPIKey)
}

func buildMusicBrainzAdapter(cf clientFactory, cfg *config.Config) *providers.MusicBrainzAdapter {
	if !cfg.HasMusicBrainz() {
		return nil
	}
	return providers.NewMusicBrainzAdapter(cf.discovery(), cfg.MusicBrainzUserAgent)
}

func buildAppleMusicAdapter(cf clientFactory, cfg *config.Config) *providers.AppleMusicAdapter {
	if !cfg.HasAppleMusic() {
		return nil
	}
	return providers.NewAppleMusicAdapter(cf.discovery())
}

func buildSpotifyAdapter(cf clientFactory, cfg *config.Config) *providers.SpotifyAdapter {
	if !cfg.HasSpotify() {
		return nil
	}
	return providers.NewSpotifyAdapter(cf.discovery())
}

func buildSoundCloudAdapter(cf clientFactory, cfg *config.Config) *providers.SoundCloudAPIAdapter {
	return newSoundCloudAdapter(cf, cfg, nil)
}

func buildSoundCloudSearchAdapter(cf clientFactory, cfg *config.Config) *providers.SoundCloudAPIAdapter {
	return newSoundCloudAdapter(cf, cfg, providers.NewSoundCloudAdapter())
}

type soundCloudSearchFallback interface {
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

func newSoundCloudAdapter(cf clientFactory, cfg *config.Config, fallback soundCloudSearchFallback) *providers.SoundCloudAPIAdapter {
	if !cfg.HasSoundCloud() {
		return nil
	}
	return providers.NewSoundCloudAPIAdapter(cf.discovery(), fallback)
}
