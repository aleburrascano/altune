package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func BuildSearchService(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discovery.EventStore,
) *discovery.SearchService {
	w := newSearchWiring(ctx, cfg, nil)
	return w.service(contentServiceOptions(w, cfg, pool, redisClient, eventStore))
}

func BuildRankingOnlySearchService(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	transport http.RoundTripper,
) *discovery.SearchService {
	w := newSearchWiring(ctx, cfg, transport)
	return w.service(rankingOnlyServiceOptions(w, cfg, pool, redisClient))
}

type searchWiring struct {
	cf        clientFactory
	sharedMB  *discovery.MusicBrainzAdapter
	providers []discovery.SearchProvider
	breaker   *discovery.CircuitBreaker
}

func newSearchWiring(ctx context.Context, cfg *config.Config, transport http.RoundTripper) searchWiring {
	cf := newClientFactory(transport)
	sharedMB := buildMusicBrainzAdapter(cf, cfg)
	return searchWiring{
		cf:        cf,
		sharedMB:  sharedMB,
		providers: buildSearchProviderList(ctx, cf, cfg, sharedMB),
		breaker:   discovery.NewCircuitBreaker(),
	}
}

func (w searchWiring) service(opts []discovery.SearchOption) *discovery.SearchService {
	return discovery.NewSearchService(w.providers, w.breaker, opts...)
}

func contentServiceOptions(
	w searchWiring,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discovery.EventStore,
) []discovery.SearchOption {
	opts := baseSearchOptions(cfg, pool)
	opts = append(opts, explorationSearchOptions(cfg)...)
	opts = append(opts, discovery.WithSearchDeadline(cfg.DiscoverySearchDeadline))
	opts = append(opts, contentSearchOptions(w.cf, cfg, pool, redisClient, w.sharedMB, w.breaker)...)
	opts = append(opts, resultCacheSearchOptions(cfg, redisClient)...)
	return append(opts, sharedSearchOptions(cfg, redisClient, eventStore)...)
}

func rankingOnlyServiceOptions(
	w searchWiring,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
) []discovery.SearchOption {
	opts := baseSearchOptions(cfg, pool)
	return append(opts, sharedSearchOptions(cfg, redisClient, nil)...)
}

func sharedSearchOptions(
	cfg *config.Config,
	redisClient *goredis.Client,
	eventStore discovery.EventStore,
) []discovery.SearchOption {
	opts := cacheSearchOptions(redisClient)
	opts = append(opts, vocabularySearchOptions(redisClient)...)
	opts = append(opts, eventSearchOptions(cfg, eventStore)...)
	return opts
}

func baseSearchOptions(cfg *config.Config, pool *pgxpool.Pool) []discovery.SearchOption {
	opts := []discovery.SearchOption{
		discovery.WithHistoryRepository(discovery.NewPgxSearchHistoryRepository(pool)),
	}
	if cfg.TailDemotionEnabled {
		opts = append(opts, discovery.WithTailDemotion())
	}
	if cfg.CrossKindProminenceEnabled {
		opts = append(opts, discovery.WithCrossKindProminence())
	}
	return opts
}

func explorationSearchOptions(cfg *config.Config) []discovery.SearchOption {
	if !cfg.ExplorationEnabled {
		return nil
	}
	return []discovery.SearchOption{discovery.WithExploration(cfg.ExplorationRate)}
}

func contentSearchOptions(
	cf clientFactory,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	sharedMB *discovery.MusicBrainzAdapter,
	breaker *discovery.CircuitBreaker,
) []discovery.SearchOption {
	deezerContent := discovery.NewDeezerAdapter(cf.discovery())
	relationshipQuerier := discovery.NewPgxRelationshipQuerier(pool)
	findRelatedSvc := discovery.NewFindRelatedService(
		relationshipQuerier, deezerContent, deezerContent,
		discovery.WithFindRelatedCircuitBreaker(breaker),
	)
	opts := []discovery.SearchOption{
		discovery.WithArtworkResolver(buildArtworkChain(cf, cfg)),
		discovery.WithFindRelatedService(findRelatedSvc),
	}
	if pool != nil {
		opts = append(opts, discovery.WithFavorites(
			discovery.NewPgxFavoritesRepository(pool),
		))
		identityStore := discovery.NewRedisIdentityStore(
			discovery.NewPgxIdentityStore(pool),
			redisClient,
			cacheSignalOption(),
		)
		opts = append(opts, discovery.WithIdentityStore(identityStore))
	}
	if cfg.IdentityVerifyOnPersist && sharedMB != nil {
		verifyProviders := buildArtistContentProviders(cf, cfg)
		delete(verifyProviders, discovery.ProviderSoundCloud)
		delete(verifyProviders, discovery.ProviderLastFM)
		opts = append(opts, discovery.WithIdentityVerifier(
			discovery.NewIdentityVerifier(sharedMB, verifyProviders),
		))
	}
	return opts
}

func resultCacheSearchOptions(cfg *config.Config, redisClient *goredis.Client) []discovery.SearchOption {
	if redisClient == nil {
		return nil
	}
	return []discovery.SearchOption{
		discovery.WithResultCache(
			discovery.NewRedisResultCache(redisClient, cacheSignalOption()),
		),
		discovery.WithProviderResultCache(
			discovery.NewRedisProviderResultCache(redisClient, providerCacheTTLs(cfg), cacheSignalOption()),
		),
		discovery.WithHeldSlateCache(
			discovery.NewRedisHeldSlateCache(redisClient, cacheSignalOption()),
		),
	}
}

func providerCacheTTLs(cfg *config.Config) discovery.ProviderCacheTTLs {
	return discovery.ProviderCacheTTLs{
		Default:  cfg.DiscoveryProviderCacheTTL,
		Provider: map[discovery.ProviderName]time.Duration{discovery.ProviderMusicBrainz: cfg.DiscoveryMusicBrainzCacheTTL},
	}
}

func cacheSearchOptions(redisClient *goredis.Client) []discovery.SearchOption {
	if redisClient == nil {
		return nil
	}
	enrichmentCache := discovery.NewRedisEnrichmentCache(redisClient, cacheSignalOption())
	return []discovery.SearchOption{
		discovery.WithArtworkCache(discovery.NewRedisArtworkCache(redisClient, cacheSignalOption())),
		discovery.WithIdentityBridge(enrichmentCache),
		discovery.WithMBIDIndex(enrichmentCache),
	}
}

func vocabularySearchOptions(redisClient *goredis.Client) []discovery.SearchOption {
	vs := BuildVocabularyStore(redisClient)
	if vs == nil {
		return nil
	}
	return []discovery.SearchOption{discovery.WithVocabularyStore(vs)}
}

func eventSearchOptions(cfg *config.Config, eventStore discovery.EventStore) []discovery.SearchOption {
	if eventStore == nil {
		return nil
	}
	opts := []discovery.SearchOption{discovery.WithEventStore(eventStore)}
	if cfg.BehavioralRankingEnabled {
		if store, ok := eventStore.(discovery.BehavioralSignalStore); ok {
			opts = append(opts, discovery.WithBehavioralRanking(
				discovery.NewSatisfactionConsumer(store),
			))
		}
	}
	return opts
}

func BuildDiscoveryProviders(ctx context.Context, cfg *config.Config, transport http.RoundTripper) []discovery.SearchProvider {
	cf := newClientFactory(transport)
	mb := buildMusicBrainzAdapter(cf, cfg)
	return buildSearchProviderList(ctx, cf, cfg, mb)
}

func buildSearchProviderList(ctx context.Context, cf clientFactory, cfg *config.Config, mb *discovery.MusicBrainzAdapter) []discovery.SearchProvider {
	var providerList []discovery.SearchProvider
	providerList = append(providerList, discovery.NewDeezerAdapter(cf.discovery()))
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
	providerList = onlyChosenSearchProviders(providerList, cfg.DiscoverySearchProviders)
	slog.InfoContext(ctx, "discovery providers configured", "count", len(providerList))
	return providerList
}

func onlyChosenSearchProviders(list []discovery.SearchProvider, chosen []string) []discovery.SearchProvider {
	if len(chosen) == 0 {
		return list
	}
	return slices.DeleteFunc(list, func(p discovery.SearchProvider) bool {
		return !slices.Contains(chosen, p.Name().String())
	})
}

func buildScrapedSearchProviders(cf clientFactory, cfg *config.Config) []discovery.SearchProvider {
	var list []discovery.SearchProvider
	if sc := buildSoundCloudSearchAdapter(cf, cfg); sc != nil {
		list = append(list, sc)
	}
	if cfg.HasYouTubeMusic() {
		list = append(list, discovery.NewYouTubeMusicAdapter(cf.roundTripper()))
	}
	if cfg.HasAmazonMusic() {
		list = append(list, discovery.NewAmazonMusicAdapter(cf.discovery()))
	}
	if sp := buildSpotifyAdapter(cf, cfg); sp != nil {
		list = append(list, sp)
	}
	return list
}

func buildLastFMAdapter(cfg *config.Config, client *http.Client) *discovery.LastFmAdapter {
	if !cfg.HasLastFM() {
		return nil
	}
	return discovery.NewLastFmAdapter(client, cfg.LastFMAPIKey)
}

func buildMusicBrainzAdapter(cf clientFactory, cfg *config.Config) *discovery.MusicBrainzAdapter {
	if !cfg.HasMusicBrainz() {
		return nil
	}
	return discovery.NewMusicBrainzAdapter(cf.discovery(), cfg.MusicBrainzUserAgent)
}

func buildAppleMusicAdapter(cf clientFactory, cfg *config.Config) *discovery.AppleMusicAdapter {
	if !cfg.HasAppleMusic() {
		return nil
	}
	return discovery.NewAppleMusicAdapter(cf.discovery())
}

func buildSpotifyAdapter(cf clientFactory, cfg *config.Config) *discovery.SpotifyAdapter {
	if !cfg.HasSpotify() {
		return nil
	}
	return discovery.NewSpotifyAdapter(cf.discovery())
}

func buildSoundCloudAdapter(cf clientFactory, cfg *config.Config) *discovery.SoundCloudAPIAdapter {
	return newSoundCloudAdapter(cf, cfg, nil)
}

func buildSoundCloudSearchAdapter(cf clientFactory, cfg *config.Config) *discovery.SoundCloudAPIAdapter {
	return newSoundCloudAdapter(cf, cfg, discovery.NewSoundCloudAdapter())
}

type soundCloudSearchFallback interface {
	Search(ctx context.Context, query string, kinds map[discovery.ResultKind]bool) ([]discovery.SearchResult, error)
}

func newSoundCloudAdapter(cf clientFactory, cfg *config.Config, fallback soundCloudSearchFallback) *discovery.SoundCloudAPIAdapter {
	if !cfg.HasSoundCloud() {
		return nil
	}
	return discovery.NewSoundCloudAPIAdapter(cf.discovery(), fallback)
}
