package app

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"expvar"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
)

type discoveryWiring struct {
	handler        *discovery.DiscoveryHandler
	searchSvc      *discovery.SearchService
	featuredBridge *catalog.FeaturedResolver
	musicBrainz    *discovery.MusicBrainzAdapter
}

type discoveryContentStaging struct {
	featuredBridge *catalog.FeaturedResolver
	albumSvc       *discovery.GetAlbumTracksService
	artistSvc      *discovery.GetArtistContentService
	relatedSvc     *discovery.GetRelatedTracksService
	suggestSvc     *discovery.SuggestService
}

func (a *App) wireDiscoveryContent(
	cf clientFactory,
	sharedMB *discovery.MusicBrainzAdapter,
	vocabStore discovery.VocabularyStore,
	breaker *discovery.CircuitBreaker,
	eventStore discovery.EventStore,
) discoveryContentStaging {
	albumSvc, relatedSvc := a.buildAlbumAndRelatedServices(cf, breaker)
	return discoveryContentStaging{
		featuredBridge: buildFeaturedBridge(cf, sharedMB),
		albumSvc:       albumSvc,
		artistSvc:      a.buildArtistContentService(cf, sharedMB, breaker, eventStore),
		relatedSvc:     relatedSvc,
		suggestSvc:     discovery.NewSuggestService(vocabStore),
	}
}

func buildFeaturedBridge(cf clientFactory, sharedMB *discovery.MusicBrainzAdapter) *catalog.FeaturedResolver {
	featuredDeezer := discovery.NewDeezerAdapter(cf.discovery())
	featuredResolver := discovery.NewFeaturedArtistResolver(nil, featuredDeezer)
	if sharedMB != nil {
		featuredResolver = discovery.NewFeaturedArtistResolver(sharedMB, featuredDeezer)
	}
	return catalog.NewFeaturedResolver(featuredResolver)
}

func (a *App) buildAlbumAndRelatedServices(
	cf clientFactory,
	breaker *discovery.CircuitBreaker,
) (*discovery.GetAlbumTracksService, *discovery.GetRelatedTracksService) {
	deezerContent := discovery.NewDeezerAdapter(cf.discovery())
	albumProviders, relatedProviders := a.buildAlbumContentProviders(cf, deezerContent)

	relatedSvc := discovery.NewGetRelatedTracksService(relatedProviders,
		discovery.WithRelatedCircuitBreaker(breaker))
	albumSvc := discovery.NewGetAlbumTracksService(
		albumProviders,
		discovery.WithTrackFeatured(deezerContent),
		discovery.WithAlbumFallbackSearcher(deezerContent),
		discovery.WithAlbumCircuitBreaker(breaker),
	)
	return albumSvc, relatedSvc
}

func (a *App) buildAlbumContentProviders(
	cf clientFactory,
	deezerContent *discovery.DeezerAdapter,
) (map[discovery.ProviderName]discovery.AlbumContentProvider, map[string]discovery.RelatedTracksProvider) {
	itunesContent := discovery.NewITunesAdapter(cf.discovery())
	albumProviders := map[discovery.ProviderName]discovery.AlbumContentProvider{
		discovery.ProviderDeezer: deezerContent,
		discovery.ProviderITunes: itunesContent,
	}
	relatedProviders := map[string]discovery.RelatedTracksProvider{}
	if am := buildAppleMusicAdapter(cf, a.cfg); am != nil {
		albumProviders[discovery.ProviderAppleMusic] = am
	}
	if sp := buildSpotifyAdapter(cf, a.cfg); sp != nil {
		albumProviders[discovery.ProviderSpotify] = sp
	}
	if soundcloudContent := buildSoundCloudAdapter(cf, a.cfg); soundcloudContent != nil {
		albumProviders[discovery.ProviderSoundCloud] = soundcloudContent
		relatedProviders[string(discovery.ProviderKeySoundCloud)] = soundcloudContent
	}
	return albumProviders, relatedProviders
}

func (a *App) buildArtistContentService(
	cf clientFactory,
	sharedMB *discovery.MusicBrainzAdapter,
	breaker *discovery.CircuitBreaker,
	eventStore discovery.EventStore,
) *discovery.GetArtistContentService {
	artistProviders := buildArtistContentProviders(cf, a.cfg)
	artistContentOpts := a.buildArtistContentOptions(sharedMB, breaker, eventStore)
	return discovery.NewGetArtistContentService(artistProviders, artistContentOpts...)
}

func (a *App) buildArtistContentOptions(
	sharedMB *discovery.MusicBrainzAdapter,
	breaker *discovery.CircuitBreaker,
	eventStore discovery.EventStore,
) []discovery.ArtistContentOption {
	artistContentOpts := []discovery.ArtistContentOption{
		discovery.WithContentCircuitBreaker(breaker),
	}
	if eventStore != nil {
		artistContentOpts = append(artistContentOpts, discovery.WithContentEventStore(eventStore))
	}
	if opt := a.contentIdentityStoreOption(); opt != nil {
		artistContentOpts = append(artistContentOpts, opt)
	}
	if sharedMB != nil {
		artistContentOpts = append(artistContentOpts, discovery.WithMBAnchor(sharedMB))
	}
	return artistContentOpts
}

func (a *App) contentIdentityStoreOption() discovery.ArtistContentOption {
	if a.pool == nil {
		return nil
	}
	return discovery.WithContentIdentityStore(
		discovery.NewRedisIdentityStore(
			discovery.NewPgxIdentityStore(a.pool),
			a.redisClient,
			cacheSignalOption(),
		),
	)
}

func (a *App) wireDiscoveryEnrichment(cf clientFactory, sharedMB *discovery.MusicBrainzAdapter) *discovery.EnrichmentService {
	if sharedMB == nil {
		return nil
	}
	enrichmentCache := discovery.NewRedisEnrichmentCache(a.redisClient, cacheSignalOption())
	return discovery.NewEnrichmentService(
		sharedMB,
		buildArtworkChain(cf, a.cfg),
		enrichmentCache,
		discovery.WithMBIDMemo(enrichmentCache),
	)
}

func (a *App) startDiscoveryBackgroundJobs(
	ctx context.Context,
	cf clientFactory,
	searchSvc *discovery.SearchService,
	eventStore *discovery.PgxEventStore,
	vocabStore discovery.VocabularyStore,
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

func (a *App) searchActivityOptions() []discovery.SearchOption {
	if a.eventTap == nil {
		return nil
	}
	return []discovery.SearchOption{discovery.WithSearchActivityFeed(a.eventTap)}
}

func (a *App) recordEventActivityOptions() []func(*discovery.RecordEventService) {
	if a.eventTap == nil {
		return nil
	}
	return []func(*discovery.RecordEventService){discovery.WithRecordEventActivityFeed(a.eventTap)}
}

func (a *App) buildDiscoveryHandler(ctx context.Context, cf clientFactory, services discovery.DiscoveryServices) *discovery.DiscoveryHandler {
	discoveryH := discovery.NewDiscoveryHandler(services)
	discoveryH.WithDetailEnrichers(a.buildDetailEnrichers(cf))
	discoveryH.WithUnreleased(a.buildUnreleasedService(ctx, cf, services.Artist))
	if telemetry.Enabled(os.Getenv) {
		discoveryH.WithClientEvents(telemetry.NewClientEvents(a.cfg.TelemetryUserKey, otel.GetLoggerProvider().Logger(telemetry.ScopeName)))
	}
	return discoveryH
}

func (a *App) buildUnreleasedService(ctx context.Context, cf clientFactory, artistSvc *discovery.GetArtistContentService) *discovery.UnreleasedService {
	if (!a.cfg.HasGenius() && !a.cfg.HasTrackers()) || artistSvc == nil {
		return nil
	}
	var cache discovery.UnreleasedCache
	opts := a.unreleasedOptions(ctx, cf)
	store := a.unreleasedStore(ctx)
	if store != nil {
		opts = append(opts, discovery.WithUnreleasedStore(store))
	} else {
		cache = discovery.NewRedisUnreleasedCache(a.redisClient, cacheSignalOption())
	}
	svc := discovery.NewUnreleasedService(a.geniusSongCatalog(cf), artistSvc, cache, a.unreleasedConfig(), opts...)
	a.startUnreleasedRefresh(svc, store)
	return svc
}

func (a *App) unreleasedStore(ctx context.Context) discovery.UnreleasedStore {
	if a.cfg.UnreleasedSource == "cache" {
		return nil
	}
	if a.pool == nil {
		slog.WarnContext(ctx, "unreleased.store_unavailable_using_cache", "reason", "no database pool")
		return nil
	}
	return discovery.NewPgxUnreleasedStore(a.pool)
}

func (a *App) geniusSongCatalog(cf clientFactory) discovery.GeniusSongCatalog {
	if !a.cfg.HasGenius() {
		return nil
	}
	return discovery.NewSongCatalog(cf.discovery(), a.cfg.GeniusAccessToken)
}

func (a *App) unreleasedOptions(ctx context.Context, cf clientFactory) []discovery.UnreleasedOption {
	opts := []discovery.UnreleasedOption{discovery.WithUnreleasedLifecycle(ctx, &a.wg)}
	if a.cfg.HasTrackers() {
		index := discovery.NewRedisTrackerIndexCache(a.redisClient, cacheSignalOption())
		opts = append(opts, discovery.WithUnreleasedTrackers(discovery.NewTrackersClient(cf.discovery(), a.cfg.GoogleSheetsAPIKey, a.cfg.TrackerHubSheetID, index)))
	}
	return opts
}

func (a *App) unreleasedConfig() discovery.UnreleasedConfig {
	return discovery.UnreleasedConfig{
		MaxPages:          a.cfg.GeniusUnreleasedMaxPages,
		MaxDetails:        a.cfg.GeniusUnreleasedMaxDetails,
		DetailConcurrency: a.cfg.GeniusUnreleasedDetailConcurrency,
		TTL:               a.cfg.GeniusUnreleasedTTL,
		EmptyTTL:          a.cfg.GeniusUnreleasedEmptyTTL,
		Wait:              a.cfg.GeniusUnreleasedWait,
		JobTimeout:        a.cfg.GeniusUnreleasedJobTimeout,
	}
}

func (a *App) wireDiscovery(ctx context.Context, cf clientFactory) discoveryWiring {
	sharedMB := buildMusicBrainzAdapter(cf, a.cfg)
	historyRepo := discovery.NewPgxSearchHistoryRepository(a.pool)
	eventStore := discovery.NewPgxEventStore(a.pool)

	vocabStore := BuildVocabularyStore(a.redisClient)

	historySvc := discovery.NewListSearchHistoryService(historyRepo)
	clearHistorySvc := discovery.NewClearSearchHistoryService(historyRepo)

	searchSvc := BuildSearchServiceWithTransport(
		ctx,
		a.cfg,
		a.pool,
		a.redisClient,
		eventStore,
		cf.roundTripper(),
		vocabStore,
		a.searchActivityOptions()...,
	)
	content := a.wireDiscoveryContent(cf, sharedMB, vocabStore, searchSvc.CircuitBreaker(), eventStore)

	eventSvc := discovery.NewRecordEventService(eventStore, a.recordEventActivityOptions()...)
	favoritesSvc := discovery.NewFavoritesService(
		discovery.NewPgxFavoritesRepository(a.pool),
	)

	enrichSvc := a.wireDiscoveryEnrichment(cf, sharedMB)

	discoveryH := a.buildDiscoveryHandler(ctx, cf, discovery.DiscoveryServices{
		Search:        searchSvc,
		History:       historySvc,
		RecordHistory: discovery.NewRecordSearchHistoryService(historyRepo),
		ClearHistory:  clearHistorySvc,
		Album:         content.albumSvc,
		Artist:        content.artistSvc,
		Related:       content.relatedSvc,
		Enrich:        enrichSvc,
		Suggest:       content.suggestSvc,
		Event:         eventSvc,
		Favorites:     favoritesSvc,
	})

	a.startDiscoveryBackgroundJobs(ctx, cf, searchSvc, eventStore, vocabStore)

	return discoveryWiring{
		handler:        discoveryH,
		searchSvc:      searchSvc,
		featuredBridge: content.featuredBridge,
		musicBrainz:    sharedMB,
	}
}

const cacheOpsVarName = "discovery_cache_ops"

func cacheSignal() *discovery.CacheSignal {
	ops, ok := expvar.Get(cacheOpsVarName).(*expvar.Map)
	if !ok {
		ops = expvar.NewMap(cacheOpsVarName)
	}
	return discovery.NewCacheSignal(ops, time.Now)
}

func cacheSignalOption() discovery.CacheOption {
	return discovery.WithCacheSignal(cacheSignal())
}
