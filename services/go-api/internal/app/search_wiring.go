package app

import (
	"context"
	"net/http"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	"altune/go-api/internal/discovery/adapters/providers"
	domain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/phonetics"
	"altune/go-api/internal/shared/textnorm"

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

	var sharedMB *providers.MusicBrainzAdapter
	if cfg.HasMusicBrainz() {
		sharedMB = providers.NewMusicBrainzAdapter(
			cf.discovery(),
			cfg.MusicBrainzUserAgent,
		)
	}

	searchProviders := buildDiscoveryProviders(cf, cfg, sharedMB)
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
	var mb *providers.MusicBrainzAdapter
	if cfg.HasMusicBrainz() {
		mb = providers.NewMusicBrainzAdapter(cf.discovery(), cfg.MusicBrainzUserAgent)
	}
	return buildDiscoveryProviders(cf, cfg, mb)
}

func BuildConsensusProviders(cfg *config.Config, transport http.RoundTripper) []discoveryService.ConsensusProvider {
	cf := clientFactory{transport: transport}
	var consensusProviders []discoveryService.ConsensusProvider

	if cfg.HasLastFM() {
		lfm := providers.NewLastFmAdapter(cf.discovery(), cfg.LastFMAPIKey)
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "lastfm",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				return lfm.GetArtistAlbums(ctx, domain.ProviderLastFM, artistName)
			},
		})
	}
	if cfg.HasMusicBrainz() {
		mb := providers.NewMusicBrainzAdapter(cf.discovery(), cfg.MusicBrainzUserAgent)
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "musicbrainz",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				return mb.ListArtistDiscography(ctx, artistName)
			},
		})
	}
	if cfg.HasDiscogs() {
		discogs := providers.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent)
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "discogs",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				info, err := discogs.ResolveDiscogsArtist(ctx, artistName, nil)
				if err != nil || info == nil {
					return nil, err
				}
				releases, err := discogs.FetchArtistReleases(ctx, info.ID)
				if err != nil {
					return nil, err
				}
				return discogsReleasesToSearchResults(releases), nil
			},
		})
	}

	itunes := providers.NewITunesAdapter(cf.discovery())
	consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
		Name: "itunes",
		Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
			return itunes.Search(ctx, artistName, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
		},
	})

	ytmusic := providers.NewYouTubeMusicAdapter(cf.roundTripper())
	consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
		Name: "ytmusic",
		Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
			return ytmusic.GetArtistAlbums(ctx, domain.ProviderYouTube, artistName)
		},
	})

	sc := providers.NewSoundCloudAPIAdapter(cf.discovery(), nil)
	consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
		Name: "soundcloud",
		Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
			return sc.Search(ctx, artistName, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
		},
	})

	return consensusProviders
}

// discogsReleasesToSearchResults maps Discogs artist releases onto album
// SearchResults, carrying the year and record type through as extras.
func discogsReleasesToSearchResults(releases []discoveryPorts.DiscogsRelease) []domain.SearchResult {
	results := make([]domain.SearchResult, 0, len(releases))
	for _, r := range releases {
		results = append(results, domain.SearchResult{
			Kind:  domain.ResultKindAlbum,
			Title: r.Title,
			Extras: map[string]any{
				"year":        r.Year,
				"record_type": r.Type,
			},
		})
	}
	return results
}

func buildArtworkChain(cf clientFactory, cfg *config.Config) discoveryPorts.TaggingArtworkResolver {
	var artworkResolvers []discoveryPorts.ArtworkResolver
	artworkResolvers = append(artworkResolvers,
		providers.NewCoverArtArchiveResolver(cf.discovery()))
	artworkResolvers = append(artworkResolvers,
		providers.NewSpotifyArtworkResolver(cf.discovery()))
	if cfg.HasDiscogs() {
		artworkResolvers = append(artworkResolvers,
			providers.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent))
	}
	if cfg.HasFanartTV() {
		artworkResolvers = append(artworkResolvers,
			providers.NewFanartTvArtworkResolver(cf.discovery(), cfg.FanartTVAPIKey))
	}
	if cfg.HasGenius() {
		artworkResolvers = append(artworkResolvers,
			providers.NewGeniusArtworkResolver(cf.discovery(), cfg.GeniusAccessToken))
	}
	artworkResolvers = append(artworkResolvers,
		providers.NewTheAudioDBAdapter(cf.discovery()),
		providers.NewDeezerAdapter(cf.discovery()),
		providers.NewITunesAdapter(cf.discovery()),
		providers.NewYouTubeMusicArtworkResolver(cf.roundTripper()),
	)
	artworkResolvers = append(artworkResolvers,
		providers.NewSoundCloudAPIAdapter(cf.discovery(), nil))
	return providers.NewChainedArtworkResolver(artworkResolvers...)
}

func BuildVocabularyStore(redisClient *goredis.Client) discoveryPorts.VocabularyStore {
	if redisClient == nil {
		return nil
	}
	return discoveryCacheAdapters.NewVocabularyStore(
		redisClient,
		textnorm.NormalizeForMatch,
		discoveryCacheAdapters.WithMetaphone(phonetics.MetaphoneKey),
	)
}
