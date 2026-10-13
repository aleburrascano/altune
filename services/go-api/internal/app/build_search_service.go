package app

import (
	"altune/go-api/internal/discovery/adapters/providers/amazonmusic"
	"altune/go-api/internal/discovery/adapters/providers/applemusic"
	"altune/go-api/internal/discovery/adapters/providers/deezer"
	"altune/go-api/internal/discovery/adapters/providers/lastfm"
	"altune/go-api/internal/discovery/adapters/providers/musicbrainz"
	"altune/go-api/internal/discovery/adapters/providers/soundcloud"
	"altune/go-api/internal/discovery/adapters/providers/spotify"
	"altune/go-api/internal/discovery/adapters/providers/ytmusic"
	"altune/go-api/internal/discovery/service/identity"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/rank"
	"altune/go-api/internal/discovery/service/related"
	"altune/go-api/internal/discovery/service/search"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"net/http"
	"slices"
	"time"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	domain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func BuildSearchService(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
) *search.Service {
	return BuildSearchServiceWithTransport(ctx, cfg, pool, redisClient, eventStore, nil, nil)
}

func BuildSearchServiceWithTransport(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
	transport http.RoundTripper,
	vocabStore discoveryPorts.VocabularyStore,
	extra ...search.Option,
) *search.Service {
	w := newSearchWiring(ctx, cfg, transport)
	opts := contentServiceOptions(w, cfg, pool, redisClient, eventStore, vocabStore)
	return w.service(append(opts, extra...))
}

func BuildRankingOnlySearchService(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	transport http.RoundTripper,
) *search.Service {
	w := newSearchWiring(ctx, cfg, transport)
	return w.service(rankingOnlyServiceOptions(w, cfg, pool, redisClient))
}

type searchWiring struct {
	cf        clientFactory
	sharedMB  *musicbrainz.MusicBrainzAdapter
	providers []discoveryPorts.SearchProvider
	breaker   *providercall.CircuitBreaker
}

func newSearchWiring(ctx context.Context, cfg *config.Config, transport http.RoundTripper) searchWiring {
	cf := newClientFactory(transport)
	sharedMB := buildMusicBrainzAdapter(cf, cfg)
	return searchWiring{
		cf:        cf,
		sharedMB:  sharedMB,
		providers: buildSearchProviderList(ctx, cf, cfg, sharedMB),
		breaker:   providercall.NewCircuitBreaker(),
	}
}

func (w searchWiring) service(opts []search.Option) *search.Service {
	return search.NewService(w.providers, w.breaker, opts...)
}

func contentServiceOptions(
	w searchWiring,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
	vocabStore discoveryPorts.VocabularyStore,
) []search.Option {
	opts := baseSearchOptions(cfg, pool)
	opts = append(opts, explorationSearchOptions(cfg)...)
	opts = append(opts, search.WithSearchDeadline(cfg.DiscoverySearchDeadline))
	opts = append(opts, contentSearchOptions(w.cf, cfg, pool, redisClient, w.sharedMB, w.breaker)...)
	opts = append(opts, resultCacheSearchOptions(cfg, redisClient)...)
	return append(opts, sharedSearchOptions(w, cfg, redisClient, eventStore, vocabStore)...)
}

func rankingOnlyServiceOptions(
	w searchWiring,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
) []search.Option {
	opts := baseSearchOptions(cfg, pool)
	return append(opts, sharedSearchOptions(w, cfg, redisClient, nil, nil)...)
}

func sharedSearchOptions(
	w searchWiring,
	cfg *config.Config,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
	vocabStore discoveryPorts.VocabularyStore,
) []search.Option {
	opts := cacheSearchOptions(redisClient)
	opts = append(opts, vocabularySearchOptions(redisClient, vocabStore)...)
	opts = append(opts, eventSearchOptions(cfg, eventStore)...)
	return opts
}

func baseSearchOptions(cfg *config.Config, pool *pgxpool.Pool) []search.Option {
	opts := []search.Option{
		search.WithHistoryRepository(discoveryPersistence.NewPgxSearchHistoryRepository(pool)),
	}
	if cfg.TailDemotionEnabled {
		opts = append(opts, search.WithTailDemotion())
	}
	if cfg.CrossKindProminenceEnabled {
		opts = append(opts, search.WithCrossKindProminence())
	}
	return opts
}

func explorationSearchOptions(cfg *config.Config) []search.Option {
	if !cfg.ExplorationEnabled {
		return nil
	}
	return []search.Option{search.WithExploration(cfg.ExplorationRate)}
}

func contentSearchOptions(
	cf clientFactory,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	sharedMB *musicbrainz.MusicBrainzAdapter,
	breaker *providercall.CircuitBreaker,
) []search.Option {
	deezerContent := deezer.NewDeezerAdapter(cf.discovery())
	relationshipQuerier := discoveryPersistence.NewPgxRelationshipQuerier(pool)
	findRelatedSvc := related.NewFindRelatedService(
		relationshipQuerier, deezerContent, deezerContent,
		related.WithFindRelatedCircuitBreaker(breaker),
	)
	opts := []search.Option{
		search.WithArtworkResolver(buildArtworkChain(cf, cfg)),
		search.WithFindRelatedService(findRelatedSvc),
	}
	if pool != nil {
		opts = append(opts, search.WithFavorites(
			discoveryPersistence.NewPgxFavoritesRepository(pool),
		))
		identityStore := discoveryCacheAdapters.NewRedisIdentityStore(
			discoveryPersistence.NewPgxIdentityStore(pool),
			redisClient,
			cacheSignalOption(),
		)
		opts = append(opts, search.WithIdentityStore(identityStore))
	}
	if cfg.IdentityVerifyOnPersist && sharedMB != nil {
		verifyProviders := buildArtistContentProviders(cf, cfg)
		delete(verifyProviders, domain.ProviderSoundCloud)
		opts = append(opts, search.WithIdentityVerifier(
			identity.NewIdentityVerifier(sharedMB, verifyProviders),
		))
	}
	return opts
}

func resultCacheSearchOptions(cfg *config.Config, redisClient *goredis.Client) []search.Option {
	if redisClient == nil {
		return nil
	}
	return []search.Option{
		search.WithResultCache(
			discoveryCacheAdapters.NewRedisResultCache(redisClient, cacheSignalOption()),
		),
		search.WithProviderResultCache(
			discoveryCacheAdapters.NewRedisProviderResultCache(redisClient, providerCacheTTLs(cfg), cacheSignalOption()),
		),
		search.WithHeldSlateCache(
			discoveryCacheAdapters.NewRedisHeldSlateCache(redisClient, cacheSignalOption()),
		),
	}
}

func providerCacheTTLs(cfg *config.Config) discoveryCacheAdapters.ProviderCacheTTLs {
	return discoveryCacheAdapters.ProviderCacheTTLs{
		Default:  cfg.DiscoveryProviderCacheTTL,
		Provider: map[domain.ProviderName]time.Duration{domain.ProviderMusicBrainz: cfg.DiscoveryMusicBrainzCacheTTL},
	}
}

func cacheSearchOptions(redisClient *goredis.Client) []search.Option {
	if redisClient == nil {
		return nil
	}
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(redisClient, cacheSignalOption())
	return []search.Option{
		search.WithArtworkCache(discoveryCacheAdapters.NewRedisArtworkCache(redisClient, cacheSignalOption())),
		search.WithIdentityBridge(enrichmentCache),
		search.WithMBIDIndex(enrichmentCache),
	}
}

func vocabularySearchOptions(redisClient *goredis.Client, vocabStore discoveryPorts.VocabularyStore) []search.Option {
	vs := vocabStore
	if vs == nil {
		vs = BuildVocabularyStore(redisClient)
	}
	if vs == nil {
		return nil
	}
	return []search.Option{search.WithVocabularyStore(vs)}
}

func eventSearchOptions(cfg *config.Config, eventStore discoveryPorts.EventStore) []search.Option {
	if eventStore == nil {
		return nil
	}
	opts := []search.Option{search.WithEventStore(eventStore)}
	if cfg.BehavioralRankingEnabled {
		if store, ok := eventStore.(discoveryPorts.BehavioralSignalStore); ok {
			opts = append(opts, search.WithBehavioralRanking(
				rank.NewSatisfactionConsumer(store),
			))
		}
	}
	return opts
}

func BuildDiscoveryProviders(ctx context.Context, cfg *config.Config, transport http.RoundTripper) []discoveryPorts.SearchProvider {
	cf := newClientFactory(transport)
	mb := buildMusicBrainzAdapter(cf, cfg)
	return buildSearchProviderList(ctx, cf, cfg, mb)
}

func buildSearchProviderList(ctx context.Context, cf clientFactory, cfg *config.Config, mb *musicbrainz.MusicBrainzAdapter) []discoveryPorts.SearchProvider {
	var providerList []discoveryPorts.SearchProvider
	providerList = append(providerList, deezer.NewDeezerAdapter(cf.discovery()))
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

func onlyChosenSearchProviders(list []discoveryPorts.SearchProvider, chosen []string) []discoveryPorts.SearchProvider {
	if len(chosen) == 0 {
		return list
	}
	return slices.DeleteFunc(list, func(p discoveryPorts.SearchProvider) bool {
		return !slices.Contains(chosen, p.Name().String())
	})
}

func buildScrapedSearchProviders(cf clientFactory, cfg *config.Config) []discoveryPorts.SearchProvider {
	var list []discoveryPorts.SearchProvider
	if sc := buildSoundCloudSearchAdapter(cf, cfg); sc != nil {
		list = append(list, sc)
	}
	if cfg.HasYouTubeMusic() {
		list = append(list, ytmusic.NewYouTubeMusicAdapter(cf.roundTripper()))
	}
	if cfg.HasAmazonMusic() {
		list = append(list, amazonmusic.NewAmazonMusicAdapter(cf.discovery()))
	}
	if sp := buildSpotifyAdapter(cf, cfg); sp != nil {
		list = append(list, sp)
	}
	return list
}

func buildLastFMAdapter(cfg *config.Config, client *http.Client) *lastfm.LastFmAdapter {
	if !cfg.HasLastFM() {
		return nil
	}
	return lastfm.NewLastFmAdapter(client, cfg.LastFMAPIKey)
}

func buildMusicBrainzAdapter(cf clientFactory, cfg *config.Config) *musicbrainz.MusicBrainzAdapter {
	if !cfg.HasMusicBrainz() {
		return nil
	}
	return musicbrainz.NewMusicBrainzAdapter(cf.discovery(), cfg.MusicBrainzUserAgent)
}

func buildAppleMusicAdapter(cf clientFactory, cfg *config.Config) *applemusic.AppleMusicAdapter {
	if !cfg.HasAppleMusic() {
		return nil
	}
	return applemusic.NewAppleMusicAdapter(cf.discovery())
}

func buildSpotifyAdapter(cf clientFactory, cfg *config.Config) *spotify.SpotifyAdapter {
	if !cfg.HasSpotify() {
		return nil
	}
	return spotify.NewSpotifyAdapter(cf.discovery())
}

func buildSoundCloudAdapter(cf clientFactory, cfg *config.Config) *soundcloud.SoundCloudAPIAdapter {
	return newSoundCloudAdapter(cf, cfg, nil)
}

func buildSoundCloudSearchAdapter(cf clientFactory, cfg *config.Config) *soundcloud.SoundCloudAPIAdapter {
	return newSoundCloudAdapter(cf, cfg, soundcloud.NewSoundCloudAdapter())
}

type soundCloudSearchFallback interface {
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

func newSoundCloudAdapter(cf clientFactory, cfg *config.Config, fallback soundCloudSearchFallback) *soundcloud.SoundCloudAPIAdapter {
	if !cfg.HasSoundCloud() {
		return nil
	}
	return soundcloud.NewSoundCloudAPIAdapter(cf.discovery(), fallback)
}
