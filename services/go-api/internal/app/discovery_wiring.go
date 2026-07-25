package app

import (
	"context"
	"time"

	"log/slog"

	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/catalog/adapters/discoverybridge"
	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	"altune/go-api/internal/discovery/adapters/providers"
	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
)

type discoveryWiring struct {
	handler        *discoveryHandler.DiscoveryHandler
	requestStore   *requeststore.Store
	searchSvc      *discoveryService.Service
	artistSvc      *discoveryService.GetArtistContentService
	featuredBridge *discoverybridge.FeaturedResolver
}

type discoveryContentWiring struct {
	featuredBridge *discoverybridge.FeaturedResolver
	albumSvc       *discoveryService.GetAlbumTracksService
	artistSvc      *discoveryService.GetArtistContentService
	relatedSvc     *discoveryService.GetRelatedTracksService
	suggestSvc     *discoveryService.SuggestService
}

func (a *App) wireDiscoveryConsensus(sharedMB *providers.MusicBrainzAdapter) *discoveryService.ConsensusService {
	consensusProviders := BuildConsensusProviders(a.cfg, nil)

	var consensusOpts []discoveryService.ConsensusOption
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
	sharedMB *providers.MusicBrainzAdapter,
	vocabStore discoveryPorts.VocabularyStore,
	consensusSvc *discoveryService.ConsensusService,
) discoveryContentWiring {
	featuredDeezer := providers.NewDeezerAdapter(newDiscoveryClient())
	featuredResolver := discoveryService.NewFeaturedArtistResolver(nil, featuredDeezer)
	if sharedMB != nil {
		featuredResolver = discoveryService.NewFeaturedArtistResolver(sharedMB, featuredDeezer)
	}
	featuredBridge := discoverybridge.NewFeaturedResolver(featuredResolver)

	deezerContentClient := newDiscoveryClient()
	deezerContent := providers.NewDeezerAdapter(deezerContentClient)
	itunesContent := providers.NewITunesAdapter(newDiscoveryClient())
	appleMusicContent := providers.NewAppleMusicAdapter(newDiscoveryClient())
	spotifyContent := providers.NewSpotifyAdapter(newDiscoveryClient())
	soundcloudContent := providers.NewSoundCloudAPIAdapter(newDiscoveryClient(), nil)

	albumProviders := map[discoveryDomain.ProviderName]discoveryPorts.AlbumContentProvider{
		discoveryDomain.ProviderDeezer:     deezerContent,
		discoveryDomain.ProviderITunes:     itunesContent,
		discoveryDomain.ProviderAppleMusic: appleMusicContent,
		discoveryDomain.ProviderSpotify:    spotifyContent,
		discoveryDomain.ProviderSoundCloud: soundcloudContent,
	}
	artistProviders := map[discoveryDomain.ProviderName]discoveryPorts.ArtistContentProvider{
		discoveryDomain.ProviderDeezer:     deezerContent,
		discoveryDomain.ProviderAppleMusic: appleMusicContent,
		discoveryDomain.ProviderSpotify:    spotifyContent,
		discoveryDomain.ProviderSoundCloud: soundcloudContent,
	}
	if a.cfg.HasLastFM() {
		artistProviders[discoveryDomain.ProviderLastFM] = providers.NewLastFmAdapter(newDiscoveryClient(), a.cfg.LastFMAPIKey)
	}

	relatedProviders := map[string]discoveryPorts.RelatedTracksProvider{
		"soundcloud": soundcloudContent,
	}
	relatedSvc := discoveryService.NewGetRelatedTracksService(relatedProviders)

	albumSvc := discoveryService.NewGetAlbumTracksService(
		albumProviders,
		discoveryService.WithTrackFeatured(deezerContent),
		discoveryService.WithAlbumFallbackSearcher(deezerContent),
	)

	var artistContentOpts []discoveryService.ArtistContentOption
	artistContentOpts = append(artistContentOpts, discoveryService.WithConsensusService(consensusSvc))
	if a.pool != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithContentIdentityStore(
			discoveryCacheAdapters.NewRedisIdentityStore(
				discoveryPersistence.NewPgxIdentityStore(a.pool),
				a.redisClient,
			),
		))
	}
	if sharedMB != nil {
		artistContentOpts = append(artistContentOpts, discoveryService.WithMBAnchor(sharedMB))
	}
	artistSvc := discoveryService.NewGetArtistContentService(artistProviders, artistContentOpts...)
	suggestSvc := discoveryService.NewSuggestService(vocabStore)

	return discoveryContentWiring{
		featuredBridge: featuredBridge,
		albumSvc:       albumSvc,
		artistSvc:      artistSvc,
		relatedSvc:     relatedSvc,
		suggestSvc:     suggestSvc,
	}
}

func (a *App) wireDiscoveryEnrichment(sharedMB *providers.MusicBrainzAdapter) *discoveryService.EnrichmentService {
	if sharedMB == nil {
		return nil
	}
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(a.redisClient)
	return discoveryService.NewEnrichmentService(
		sharedMB,
		buildArtworkChain(clientFactory{}, a.cfg),
		enrichmentCache,
		discoveryService.WithMBIDMemo(enrichmentCache),
	)
}

func (a *App) wireDiscovery(ctx context.Context) discoveryWiring {
	var sharedMB *providers.MusicBrainzAdapter
	if a.cfg.HasMusicBrainz() {
		sharedMB = providers.NewMusicBrainzAdapter(
			newDiscoveryClient(),
			a.cfg.MusicBrainzUserAgent,
		)
	}
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(a.pool)
	eventStore := discoveryPersistence.NewPgxEventStore(a.pool)

	vocabStore := BuildVocabularyStore(a.redisClient)

	historySvc := discoveryService.NewListSearchHistoryService(historyRepo)
	clearHistorySvc := discoveryService.NewClearSearchHistoryService(historyRepo)

	consensusSvc := a.wireDiscoveryConsensus(sharedMB)
	content := a.wireDiscoveryContent(sharedMB, vocabStore, consensusSvc)

	requestStore := requeststore.New()
	searchSvc := BuildSearchServiceWithTransport(
		a.cfg,
		a.pool,
		a.redisClient,
		eventStore,
		requeststore.NewTransport(defaultLiveTransport, requestStore),
		vocabStore,
		false,
	)

	if a.cfg.BehavioralRankingEnabled {
		a.whenLeader(func(ctx context.Context) {
			searchSvc.StartBehavioralRefresh(ctx, 30*time.Minute)
			slog.Info("behavioral ranking refresh started")
		})
	}

	a.startCorpusRefresh(ctx, eventStore)

	a.startMetricsRollup(ctx, discoveryPersistence.NewPgxMetricsRollup(a.pool))

	eventSvc := discoveryService.NewRecordEventService(eventStore)

	enrichSvc := a.wireDiscoveryEnrichment(sharedMB)

	discoveryH := discoveryHandler.NewDiscoveryHandler(discoveryHandler.DiscoveryServices{
		Search:       searchSvc,
		History:      historySvc,
		ClearHistory: clearHistorySvc,
		Album:        content.albumSvc,
		Artist:       content.artistSvc,
		Related:      content.relatedSvc,
		Enrich:       enrichSvc,
		Suggest:      content.suggestSvc,
		Event:        eventSvc,
	})
	discoveryH.WithDetailEnrichers(a.buildDetailEnrichers())
	a.providerHealth = providerhealth.NewStore()
	discoveryH.WithProviderHealth(a.providerHealth)
	discoveryH.WithRequestTrace(requestStore)

	a.startVocabularyRefresh(vocabStore)

	return discoveryWiring{
		handler:        discoveryH,
		requestStore:   requestStore,
		searchSvc:      searchSvc,
		artistSvc:      content.artistSvc,
		featuredBridge: content.featuredBridge,
	}
}
