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

// BuildSearchService builds the production, content-serving search service
// over the default live transport, with the Redis-backed vocabulary store.
func BuildSearchService(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
) *discoveryService.Service {
	return BuildSearchServiceWithTransport(cfg, pool, redisClient, eventStore, nil, nil)
}

// BuildSearchServiceWithTransport builds the production, content-serving
// search service over the given transport: ranking plus exploration, artwork,
// related lookups, favorites, identity and the result cache. A nil transport
// uses the default; a nil vocabStore falls back to the Redis-backed store.
func BuildSearchServiceWithTransport(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
	transport http.RoundTripper,
	vocabStore discoveryPorts.VocabularyStore,
) *discoveryService.Service {
	w := newSearchWiring(cfg, transport)
	return w.service(contentServiceOptions(w, cfg, pool, redisClient, eventStore, vocabStore))
}

// BuildRankingOnlySearchService builds the search service used by evals and
// fixture record/replay: ranking only, without exploration, the
// content-serving concerns, the result cache or an event store.
// A nil transport uses the default.
func BuildRankingOnlySearchService(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	transport http.RoundTripper,
) *discoveryService.Service {
	w := newSearchWiring(cfg, transport)
	return w.service(rankingOnlyServiceOptions(w, cfg, pool, redisClient))
}

// searchWiring holds the pieces both search service shapes share: the client
// factory over the chosen transport, the shared MusicBrainz adapter, the
// search provider list and the circuit breaker.
type searchWiring struct {
	cf        clientFactory
	sharedMB  *providers.MusicBrainzAdapter
	providers []discoveryPorts.SearchProvider
	breaker   *discoveryService.CircuitBreaker
}

func newSearchWiring(cfg *config.Config, transport http.RoundTripper) searchWiring {
	cf := clientFactory{transport: transport}
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

// contentServiceOptions composes the production option set.
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

// rankingOnlyServiceOptions composes the eval/fixture option set.
func rankingOnlyServiceOptions(
	w searchWiring,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
) []discoveryService.Option {
	opts := baseSearchOptions(cfg, pool)
	return append(opts, sharedSearchOptions(w, cfg, redisClient, nil, nil)...)
}

// sharedSearchOptions wires the trailing concerns both shapes carry: the
// ranking-side caches, vocabulary, events and the album validator.
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

// baseSearchOptions wires the ranking concerns present in both shapes:
// history persistence plus the independently-toggleable tail-demotion and
// cross-kind-prominence ranking tweaks.
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

// explorationSearchOptions wires the exploration ranking tweak, which only
// the content-serving shape carries.
func explorationSearchOptions(cfg *config.Config) []discoveryService.Option {
	if !cfg.ExplorationEnabled {
		return nil
	}
	return []discoveryService.Option{discoveryService.WithExploration(cfg.ExplorationRate)}
}

// contentSearchOptions wires the content-serving concerns absent from the
// ranking-only shape: artwork resolution, related lookups, favorites, the
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

// resultCacheSearchOptions wires the Redis-backed result cache, content-only.
func resultCacheSearchOptions(redisClient *goredis.Client) []discoveryService.Option {
	if redisClient == nil {
		return nil
	}
	return []discoveryService.Option{discoveryService.WithResultCache(
		discoveryCacheAdapters.NewRedisResultCache(redisClient),
	)}
}

// cacheSearchOptions wires the Redis-backed caches both shapes carry: artwork
// cache, identity bridge and MBID index.
func cacheSearchOptions(redisClient *goredis.Client) []discoveryService.Option {
	if redisClient == nil {
		return nil
	}
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(redisClient)
	return []discoveryService.Option{
		discoveryService.WithArtworkCache(discoveryCacheAdapters.NewRedisArtworkCache(redisClient)),
		discoveryService.WithIdentityBridge(enrichmentCache),
		discoveryService.WithMBIDIndex(enrichmentCache),
	}
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

	if cfg.HasAppleMusic() {
		providerList = append(providerList, providers.NewAppleMusicAdapter(cf.discovery()))
	}

	if mb != nil {
		providerList = append(providerList, mb)
	}

	if cfg.HasLastFM() {
		lfmClient := cf.discovery()
		providerList = append(providerList, providers.NewLastFmAdapter(lfmClient, cfg.LastFMAPIKey))
	}

	providerList = append(providerList, buildScrapedSearchProviders(cf, cfg)...)

	slog.Info("discovery providers configured", "count", len(providerList))
	return providerList
}

// buildScrapedSearchProviders builds the search adapters that depend on
// reverse-engineered credentials, each skipped when its kill switch is off.
func buildScrapedSearchProviders(cf clientFactory, cfg *config.Config) []discoveryPorts.SearchProvider {
	var list []discoveryPorts.SearchProvider
	if cfg.HasSoundCloud() {
		list = append(list, providers.NewSoundCloudAPIAdapter(cf.discovery(), providers.NewSoundCloudAdapter()))
	}
	if cfg.HasYouTubeMusic() {
		list = append(list, providers.NewYouTubeMusicAdapter(cf.roundTripper()))
	}
	if cfg.HasAmazonMusic() {
		list = append(list, providers.NewAmazonMusicAdapter(cf.discovery()))
	}
	if cfg.HasSpotify() {
		list = append(list, providers.NewSpotifyAdapter(cf.discovery()))
	}
	return list
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
