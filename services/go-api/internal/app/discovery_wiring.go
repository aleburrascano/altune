package app

import (
	"altune/go-api/internal/catalog/adapters/discoverybridge"
	"altune/go-api/internal/discovery/adapters/providers/deezer"
	"altune/go-api/internal/discovery/adapters/providers/genius"
	"altune/go-api/internal/discovery/adapters/providers/itunes"
	"altune/go-api/internal/discovery/adapters/providers/musicbrainz"
	"altune/go-api/internal/discovery/adapters/providers/trackers"
	"altune/go-api/internal/discovery/service/content"
	"altune/go-api/internal/discovery/service/favorites"
	"altune/go-api/internal/discovery/service/history"
	"altune/go-api/internal/discovery/service/interaction"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/related"
	"altune/go-api/internal/discovery/service/search"
	"altune/go-api/internal/discovery/service/unreleased"
	"altune/go-api/internal/discovery/service/vocabulary"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"expvar"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"
)

type discoveryWiring struct {
	handler        *discoveryHandler.DiscoveryHandler
	searchSvc      *search.Service
	featuredBridge *discoverybridge.FeaturedResolver
	musicBrainz    *musicbrainz.MusicBrainzAdapter
}

type discoveryContentStaging struct {
	featuredBridge *discoverybridge.FeaturedResolver
	albumSvc       *content.GetAlbumTracksService
	artistSvc      *content.GetArtistContentService
	relatedSvc     *related.GetRelatedTracksService
	suggestSvc     *vocabulary.SuggestService
}

func (a *App) wireDiscoveryContent(
	cf clientFactory,
	sharedMB *musicbrainz.MusicBrainzAdapter,
	vocabStore discoveryPorts.VocabularyStore,
	breaker *providercall.CircuitBreaker,
	eventStore discoveryPorts.EventStore,
) discoveryContentStaging {
	albumSvc, relatedSvc := a.buildAlbumAndRelatedServices(cf, breaker)
	return discoveryContentStaging{
		featuredBridge: buildFeaturedBridge(cf, sharedMB),
		albumSvc:       albumSvc,
		artistSvc:      a.buildArtistContentService(cf, sharedMB, breaker, eventStore),
		relatedSvc:     relatedSvc,
		suggestSvc:     vocabulary.NewSuggestService(vocabStore),
	}
}

func buildFeaturedBridge(cf clientFactory, sharedMB *musicbrainz.MusicBrainzAdapter) *discoverybridge.FeaturedResolver {
	featuredDeezer := deezer.NewDeezerAdapter(cf.discovery())
	featuredResolver := content.NewFeaturedArtistResolver(nil, featuredDeezer)
	if sharedMB != nil {
		featuredResolver = content.NewFeaturedArtistResolver(sharedMB, featuredDeezer)
	}
	return discoverybridge.NewFeaturedResolver(featuredResolver)
}

func (a *App) buildAlbumAndRelatedServices(
	cf clientFactory,
	breaker *providercall.CircuitBreaker,
) (*content.GetAlbumTracksService, *related.GetRelatedTracksService) {
	deezerContent := deezer.NewDeezerAdapter(cf.discovery())
	albumProviders, relatedProviders := a.buildAlbumContentProviders(cf, deezerContent)

	relatedSvc := related.NewGetRelatedTracksService(relatedProviders,
		related.WithRelatedCircuitBreaker(breaker))
	albumSvc := content.NewGetAlbumTracksService(
		albumProviders,
		content.WithTrackFeatured(deezerContent),
		content.WithAlbumFallbackSearcher(deezerContent),
		content.WithAlbumCircuitBreaker(breaker),
	)
	return albumSvc, relatedSvc
}

func (a *App) buildAlbumContentProviders(
	cf clientFactory,
	deezerContent *deezer.DeezerAdapter,
) (map[discoveryDomain.ProviderName]discoveryPorts.AlbumContentProvider, map[string]discoveryPorts.RelatedTracksProvider) {
	itunesContent := itunes.NewITunesAdapter(cf.discovery())
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
	sharedMB *musicbrainz.MusicBrainzAdapter,
	breaker *providercall.CircuitBreaker,
	eventStore discoveryPorts.EventStore,
) *content.GetArtistContentService {
	artistProviders := buildArtistContentProviders(cf, a.cfg)
	artistContentOpts := a.buildArtistContentOptions(sharedMB, breaker, eventStore)
	return content.NewGetArtistContentService(artistProviders, artistContentOpts...)
}

func (a *App) buildArtistContentOptions(
	sharedMB *musicbrainz.MusicBrainzAdapter,
	breaker *providercall.CircuitBreaker,
	eventStore discoveryPorts.EventStore,
) []content.ArtistContentOption {
	artistContentOpts := []content.ArtistContentOption{
		content.WithContentCircuitBreaker(breaker),
	}
	if eventStore != nil {
		artistContentOpts = append(artistContentOpts, content.WithContentEventStore(eventStore))
	}
	if opt := a.contentIdentityStoreOption(); opt != nil {
		artistContentOpts = append(artistContentOpts, opt)
	}
	if sharedMB != nil {
		artistContentOpts = append(artistContentOpts, content.WithMBAnchor(sharedMB))
	}
	return artistContentOpts
}

func (a *App) contentIdentityStoreOption() content.ArtistContentOption {
	if a.pool == nil {
		return nil
	}
	return content.WithContentIdentityStore(
		discoveryCacheAdapters.NewRedisIdentityStore(
			discoveryPersistence.NewPgxIdentityStore(a.pool),
			a.redisClient,
			cacheSignalOption(),
		),
	)
}

func (a *App) wireDiscoveryEnrichment(cf clientFactory, sharedMB *musicbrainz.MusicBrainzAdapter) *discoveryEnrich.EnrichmentService {
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
	searchSvc *search.Service,
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

func (a *App) searchActivityOptions() []search.Option {
	if a.eventTap == nil {
		return nil
	}
	return []search.Option{search.WithSearchActivityFeed(a.eventTap)}
}

func (a *App) recordEventActivityOptions() []func(*interaction.RecordEventService) {
	if a.eventTap == nil {
		return nil
	}
	return []func(*interaction.RecordEventService){interaction.WithRecordEventActivityFeed(a.eventTap)}
}

func (a *App) buildDiscoveryHandler(ctx context.Context, cf clientFactory, services discoveryHandler.DiscoveryServices) *discoveryHandler.DiscoveryHandler {
	discoveryH := discoveryHandler.NewDiscoveryHandler(services)
	discoveryH.WithDetailEnrichers(a.buildDetailEnrichers(cf))
	discoveryH.WithUnreleased(a.buildUnreleasedService(ctx, cf, services.Artist))
	if telemetry.Enabled() {
		discoveryH.WithClientEvents(telemetry.NewClientEvents(a.cfg.TelemetryUserKey, otel.GetLoggerProvider().Logger(telemetry.ScopeName)))
	}
	return discoveryH
}

func (a *App) buildUnreleasedService(ctx context.Context, cf clientFactory, artistSvc *content.GetArtistContentService) *unreleased.Service {
	if (!a.cfg.HasGenius() && !a.cfg.HasTrackers()) || artistSvc == nil {
		return nil
	}
	var cache discoveryPorts.UnreleasedCache
	opts := a.unreleasedOptions(ctx, cf)
	if store := a.unreleasedStore(ctx); store != nil {
		opts = append(opts, unreleased.WithStore(store))
	} else {
		cache = discoveryCacheAdapters.NewRedisUnreleasedCache(a.redisClient, cacheSignalOption())
	}
	return unreleased.NewService(a.geniusSongCatalog(cf), artistSvc, cache, a.unreleasedConfig(), opts...)
}

func (a *App) unreleasedStore(ctx context.Context) discoveryPorts.UnreleasedStore {
	if a.cfg.UnreleasedSource == "cache" {
		return nil
	}
	if a.pool == nil {
		slog.WarnContext(ctx, "unreleased.store_unavailable_using_cache", "reason", "no database pool")
		return nil
	}
	return discoveryPersistence.NewPgxUnreleasedStore(a.pool)
}

func (a *App) geniusSongCatalog(cf clientFactory) discoveryPorts.GeniusSongCatalog {
	if !a.cfg.HasGenius() {
		return nil
	}
	return genius.NewSongCatalog(cf.discovery(), a.cfg.GeniusAccessToken)
}

func (a *App) unreleasedOptions(ctx context.Context, cf clientFactory) []unreleased.Option {
	opts := []unreleased.Option{unreleased.WithLifecycle(ctx, &a.wg)}
	if a.cfg.HasTrackers() {
		index := discoveryCacheAdapters.NewRedisTrackerIndexCache(a.redisClient, cacheSignalOption())
		opts = append(opts, unreleased.WithTrackers(trackers.NewClient(cf.discovery(), a.cfg.GoogleSheetsAPIKey, a.cfg.TrackerHubSheetID, index)))
	}
	return opts
}

func (a *App) unreleasedConfig() unreleased.Config {
	return unreleased.Config{
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
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(a.pool)
	eventStore := discoveryPersistence.NewPgxEventStore(a.pool)

	vocabStore := BuildVocabularyStore(a.redisClient)

	historySvc := history.NewListSearchHistoryService(historyRepo)
	clearHistorySvc := history.NewClearSearchHistoryService(historyRepo)

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

	eventSvc := interaction.NewRecordEventService(eventStore, a.recordEventActivityOptions()...)
	favoritesSvc := favorites.NewFavoritesService(
		discoveryPersistence.NewPgxFavoritesRepository(a.pool),
	)

	enrichSvc := a.wireDiscoveryEnrichment(cf, sharedMB)

	discoveryH := a.buildDiscoveryHandler(ctx, cf, discoveryHandler.DiscoveryServices{
		Search:        searchSvc,
		History:       historySvc,
		RecordHistory: history.NewRecordSearchHistoryService(historyRepo),
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

func cacheSignal() *discoveryCacheAdapters.Signal {
	ops, ok := expvar.Get(cacheOpsVarName).(*expvar.Map)
	if !ok {
		ops = expvar.NewMap(cacheOpsVarName)
	}
	return discoveryCacheAdapters.NewSignal(ops, time.Now)
}

func cacheSignalOption() discoveryCacheAdapters.Option {
	return discoveryCacheAdapters.WithSignal(cacheSignal())
}
