package app

import (
	"altune/go-api/internal/catalog/adapters/discoverybridge"
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/shared"
	"context"
	"log/slog"
	"time"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"
)

type discoveryWiring struct {
	handler        *discoveryHandler.DiscoveryHandler
	searchSvc      *discoveryService.Service
	featuredBridge *discoverybridge.FeaturedResolver
	musicBrainz    *providers.MusicBrainzAdapter
}

type discoveryContentStaging struct {
	featuredBridge *discoverybridge.FeaturedResolver
	albumSvc       *discoveryService.GetAlbumTracksService
	artistSvc      *discoveryService.GetArtistContentService
	relatedSvc     *discoveryService.GetRelatedTracksService
	suggestSvc     *discoveryService.SuggestService
}

type sharedFeaturedResolver struct {
	inner *discoveryService.FeaturedArtistResolver
}

func (r sharedFeaturedResolver) Resolve(ctx context.Context, artist, title string) ([]shared.FeaturedArtist, error) {
	feats, err := r.inner.Resolve(ctx, artist, title)
	if err != nil {
		return nil, err
	}
	out := make([]shared.FeaturedArtist, 0, len(feats))
	for _, f := range feats {
		out = append(out, shared.FeaturedArtist{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID, Role: f.Role})
	}
	return out, nil
}

func (a *App) wireDiscoveryConsensus(cf clientFactory, sharedMB *providers.MusicBrainzAdapter, breaker *discoveryService.CircuitBreaker) *discoveryService.ConsensusService {
	consensusProviders := BuildConsensusProviders(a.cfg, cf.roundTripper())

	consensusOpts := []discoveryService.ConsensusOption{discoveryService.WithConsensusCircuitBreaker(breaker)}
	if sharedMB != nil {
		consensusOpts = append(consensusOpts, discoveryService.WithMBAuthority(sharedMB))
	}
	if a.redisClient != nil {
		consensusOpts = append(consensusOpts, discoveryService.WithConsensusCache(
			discoveryCacheAdapters.NewRedisNameKeyedCache[[]discoveryService.ConsensusAlbum](
				a.redisClient,
				"discovery:consensus:v1:",
				"discovery:consensus:neg:v1:",
				discoveryService.DefaultConsensusCacheTTL,
				discoveryService.DefaultConsensusCacheTTL,
				func() []discoveryService.ConsensusAlbum { return nil },
			),
		))
	}
	return discoveryService.NewConsensusService(consensusProviders, consensusOpts...)
}

func (a *App) wireDiscoveryContent(
	cf clientFactory,
	sharedMB *providers.MusicBrainzAdapter,
	vocabStore discoveryPorts.VocabularyStore,
	consensusSvc *discoveryService.ConsensusService,
	breaker *discoveryService.CircuitBreaker,
	eventStore discoveryPorts.EventStore,
) discoveryContentStaging {
	albumSvc, relatedSvc := a.buildAlbumAndRelatedServices(cf, breaker)
	return discoveryContentStaging{
		featuredBridge: buildFeaturedBridge(cf, sharedMB),
		albumSvc:       albumSvc,
		artistSvc:      a.buildArtistContentService(cf, sharedMB, consensusSvc, breaker, eventStore),
		relatedSvc:     relatedSvc,
		suggestSvc:     discoveryService.NewSuggestService(vocabStore),
	}
}

func buildFeaturedBridge(cf clientFactory, sharedMB *providers.MusicBrainzAdapter) *discoverybridge.FeaturedResolver {
	featuredDeezer := providers.NewDeezerAdapter(cf.discovery())
	featuredResolver := discoveryService.NewFeaturedArtistResolver(nil, featuredDeezer)
	if sharedMB != nil {
		featuredResolver = discoveryService.NewFeaturedArtistResolver(sharedMB, featuredDeezer)
	}
	return discoverybridge.NewFeaturedResolver(sharedFeaturedResolver{inner: featuredResolver})
}

func (a *App) buildAlbumAndRelatedServices(
	cf clientFactory,
	breaker *discoveryService.CircuitBreaker,
) (*discoveryService.GetAlbumTracksService, *discoveryService.GetRelatedTracksService) {
	deezerContent := providers.NewDeezerAdapter(cf.discovery())
	albumProviders, relatedProviders := a.buildAlbumContentProviders(cf, deezerContent)

	relatedSvc := discoveryService.NewGetRelatedTracksService(relatedProviders,
		discoveryService.WithRelatedCircuitBreaker(breaker))
	albumSvc := discoveryService.NewGetAlbumTracksService(
		albumProviders,
		discoveryService.WithTrackFeatured(deezerContent),
		discoveryService.WithAlbumFallbackSearcher(deezerContent),
		discoveryService.WithAlbumCircuitBreaker(breaker),
	)
	return albumSvc, relatedSvc
}

func (a *App) buildAlbumContentProviders(
	cf clientFactory,
	deezerContent *providers.DeezerAdapter,
) (map[discoveryDomain.ProviderName]discoveryPorts.AlbumContentProvider, map[string]discoveryPorts.RelatedTracksProvider) {
	itunesContent := providers.NewITunesAdapter(cf.discovery())
	albumProviders := map[discoveryDomain.ProviderName]discoveryPorts.AlbumContentProvider{
		discoveryDomain.ProviderDeezer: deezerContent,
		discoveryDomain.ProviderITunes: itunesContent,
	}
	relatedProviders := map[string]discoveryPorts.RelatedTracksProvider{}
	if am := buildAppleMusicAdapter(cf, a.cfg); am != nil {
		albumProviders[discoveryDomain.ProviderAppleMusic] = am
	}
	if sp := buildSpotifyAdapter(cf, a.cfg); sp != nil {
		albumProviders[discoveryDomain.ProviderSpotify] = sp
	}
	if soundcloudContent := buildSoundCloudAdapter(cf, a.cfg); soundcloudContent != nil {
		albumProviders[discoveryDomain.ProviderSoundCloud] = soundcloudContent
		relatedProviders[string(discoveryDomain.ProviderKeySoundCloud)] = soundcloudContent
	}
	return albumProviders, relatedProviders
}

func (a *App) buildArtistContentService(
	cf clientFactory,
	sharedMB *providers.MusicBrainzAdapter,
	consensusSvc *discoveryService.ConsensusService,
	breaker *discoveryService.CircuitBreaker,
	eventStore discoveryPorts.EventStore,
) *discoveryService.GetArtistContentService {
	artistProviders := buildArtistContentProviders(cf, a.cfg)
	artistContentOpts := a.buildArtistContentOptions(sharedMB, consensusSvc, breaker, eventStore)
	return discoveryService.NewGetArtistContentService(artistProviders, artistContentOpts...)
}

func (a *App) buildArtistContentOptions(
	sharedMB *providers.MusicBrainzAdapter,
	consensusSvc *discoveryService.ConsensusService,
	breaker *discoveryService.CircuitBreaker,
	eventStore discoveryPorts.EventStore,
) []discoveryService.ArtistContentOption {
	artistContentOpts := []discoveryService.ArtistContentOption{
		discoveryService.WithConsensusService(consensusSvc),
		discoveryService.WithContentCircuitBreaker(breaker),
	}
	if eventStore != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithContentEventStore(eventStore))
	}
	if opt := a.contentIdentityStoreOption(); opt != nil {
		artistContentOpts = append(artistContentOpts, opt)
	}
	if sharedMB != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithMBAnchor(sharedMB))
	}
	return artistContentOpts
}

func (a *App) contentIdentityStoreOption() discoveryService.ArtistContentOption {
	if a.pool == nil {
		return nil
	}
	return discoveryService.WithContentIdentityStore(
		discoveryCacheAdapters.NewRedisIdentityStore(
			discoveryPersistence.NewPgxIdentityStore(a.pool),
			a.redisClient,
			cacheSignalOption(),
		),
	)
}

func (a *App) wireDiscoveryEnrichment(cf clientFactory, sharedMB *providers.MusicBrainzAdapter) *discoveryEnrich.EnrichmentService {
	if sharedMB == nil {
		return nil
	}
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(a.redisClient, cacheSignalOption())
	return discoveryEnrich.NewEnrichmentService(
		sharedMB,
		buildArtworkChain(cf, a.cfg),
		enrichmentCache,
		discoveryEnrich.WithMBIDMemo(enrichmentCache),
	)
}

func (a *App) startDiscoveryBackgroundJobs(
	ctx context.Context,
	cf clientFactory,
	searchSvc *discoveryService.Service,
	eventStore *discoveryPersistence.PgxEventStore,
	vocabStore discoveryPorts.VocabularyStore,
) {
	if a.cfg.BehavioralRankingEnabled {
		a.startEveryInstanceTicker(ctx, jobBehavioralRankingRefresh, 30*time.Minute, func(ctx context.Context) error {
			if err := searchSvc.RefreshBehavioralScores(ctx); err != nil {
				slog.WarnContext(ctx, string(jobBehavioralRankingRefresh)+" failed", "error", err)
				return err
			}
			return nil
		})
	}
	a.startCorpusRefresh(ctx, eventStore)
	a.startDiscographyPrune(ctx, eventStore)
	a.startVocabularyRefresh(ctx, cf, vocabStore)
}

func (a *App) searchActivityOptions() []discoveryService.Option {
	if a.eventTap == nil {
		return nil
	}
	return []discoveryService.Option{discoveryService.WithSearchActivityFeed(a.eventTap)}
}

func (a *App) recordEventActivityOptions() []func(*discoveryService.RecordEventService) {
	if a.eventTap == nil {
		return nil
	}
	return []func(*discoveryService.RecordEventService){discoveryService.WithRecordEventActivityFeed(a.eventTap)}
}

func (a *App) buildDiscoveryHandler(cf clientFactory, services discoveryHandler.DiscoveryServices) *discoveryHandler.DiscoveryHandler {
	discoveryH := discoveryHandler.NewDiscoveryHandler(services)
	discoveryH.WithDetailEnrichers(a.buildDetailEnrichers(cf))
	return discoveryH
}

func (a *App) wireDiscovery(ctx context.Context, cf clientFactory) discoveryWiring {
	sharedMB := buildMusicBrainzAdapter(cf, a.cfg)
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(a.pool)
	eventStore := discoveryPersistence.NewPgxEventStore(a.pool)

	vocabStore := BuildVocabularyStore(a.redisClient)

	historySvc := discoveryService.NewListSearchHistoryService(historyRepo)
	clearHistorySvc := discoveryService.NewClearSearchHistoryService(historyRepo)

	searchSvc := BuildSearchServiceWithTransport(
		a.cfg,
		a.pool,
		a.redisClient,
		eventStore,
		cf.roundTripper(),
		vocabStore,
		a.searchActivityOptions()...,
	)
	consensusSvc := a.wireDiscoveryConsensus(cf, sharedMB, searchSvc.CircuitBreaker())
	content := a.wireDiscoveryContent(cf, sharedMB, vocabStore, consensusSvc, searchSvc.CircuitBreaker(), eventStore)

	eventSvc := discoveryService.NewRecordEventService(eventStore, a.recordEventActivityOptions()...)
	favoritesSvc := discoveryService.NewFavoritesService(
		discoveryPersistence.NewPgxFavoritesRepository(a.pool),
	)

	enrichSvc := a.wireDiscoveryEnrichment(cf, sharedMB)

	discoveryH := a.buildDiscoveryHandler(cf, discoveryHandler.DiscoveryServices{
		Search:       searchSvc,
		History:      historySvc,
		ClearHistory: clearHistorySvc,
		Album:        content.albumSvc,
		Artist:       content.artistSvc,
		Related:      content.relatedSvc,
		Enrich:       enrichSvc,
		Suggest:      content.suggestSvc,
		Event:        eventSvc,
		Favorites:    favoritesSvc,
	})

	a.startDiscoveryBackgroundJobs(ctx, cf, searchSvc, eventStore, vocabStore)

	return discoveryWiring{
		handler:        discoveryH,
		searchSvc:      searchSvc,
		featuredBridge: content.featuredBridge,
		musicBrainz:    sharedMB,
	}
}
