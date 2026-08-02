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
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(pool)

	opts := []discoveryService.Option{
		discoveryService.WithHistoryRepository(historyRepo),
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
	if !rankingOnly {
		deezerContent := providers.NewDeezerAdapter(cf.discovery())
		relationshipQuerier := discoveryPersistence.NewPgxRelationshipQuerier(pool)
		findRelatedSvc := discoveryService.NewFindRelatedService(relationshipQuerier, deezerContent, deezerContent)
		opts = append(opts,
			discoveryService.WithArtworkResolver(buildArtworkChain(cf, cfg)),
			discoveryService.WithFindRelatedService(findRelatedSvc),
		)
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
			verifyProviders := map[domain.ProviderName]discoveryPorts.ArtistContentProvider{
				domain.ProviderDeezer:     deezerContent,
				domain.ProviderSpotify:    providers.NewSpotifyAdapter(cf.discovery()),
				domain.ProviderAppleMusic: providers.NewAppleMusicAdapter(cf.discovery()),
			}
			opts = append(opts, discoveryService.WithIdentityVerifier(
				discoveryService.NewIdentityVerifier(sharedMB, verifyProviders),
			))
		}
	}
	if redisClient != nil {
		if !rankingOnly {
			opts = append(opts, discoveryService.WithResultCache(
				discoveryCacheAdapters.NewRedisResultCache(redisClient),
			))
		}
		opts = append(opts, discoveryService.WithArtworkCache(
			discoveryCacheAdapters.NewRedisArtworkCache(redisClient),
		))
		enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(redisClient)
		opts = append(opts, discoveryService.WithIdentityBridge(enrichmentCache))
		opts = append(opts, discoveryService.WithMBIDIndex(enrichmentCache))
	}
	vs := vocabStore
	if vs == nil {
		vs = BuildVocabularyStore(redisClient)
	}
	if vs != nil {
		opts = append(opts, discoveryService.WithVocabularyStore(vs))
	}
	if eventStore != nil {
		opts = append(opts, discoveryService.WithEventStore(eventStore))
		if cfg.BehavioralRankingEnabled {
			if store, ok := eventStore.(discoveryPorts.BehavioralSignalStore); ok {
				opts = append(opts, discoveryService.WithBehavioralRanking(
					discoveryService.NewSatisfactionConsumer(store),
				))
			}
		}
	}
	if sharedMB != nil {
		opts = append(opts, discoveryService.WithAlbumValidator(sharedMB))
	}

	return discoveryService.NewService(searchProviders, circuitBreaker, opts...)
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
				return results, nil
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
