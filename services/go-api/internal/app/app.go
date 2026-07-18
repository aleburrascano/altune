package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	acqHandler "altune/go-api/internal/acquisition/adapters/handler"
	"altune/go-api/internal/acquisition/adapters/id3"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"
	adminAlert "altune/go-api/internal/admin/alert"
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	adminHandler "altune/go-api/internal/admin/handler"
	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/auth"
	authAdapters "altune/go-api/internal/auth/adapters"
	"altune/go-api/internal/catalog/adapters/discoverybridge"
	catalogHandler "altune/go-api/internal/catalog/adapters/handler"
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/adapters/storage"
	catalogPorts "altune/go-api/internal/catalog/ports"
	catalogService "altune/go-api/internal/catalog/service"
	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	"altune/go-api/internal/discovery/adapters/providers"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"
	"altune/go-api/internal/discovery/service/eval"
	"altune/go-api/internal/playback/adapters/catalogbridge"
	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackService "altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	sharedRedis "altune/go-api/internal/shared/redis"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type App struct {
	cfg            *config.Config
	pool           *pgxpool.Pool
	redisClient    *goredis.Client
	server         *http.Server
	wg             sync.WaitGroup
	sem            chan struct{}
	scheduler      *acqService.BackgroundAcquisitionScheduler
	vocabRefresh   *discoveryService.VocabularyRefreshService
	eventBus       *events.InProcessBus
	alertMonitor   *adminAlert.Monitor
	logRing        *logging.RingBuffer
	eventFeed      *eventtap.Feed
	providerHealth *providerhealth.Store
	evalMeter      *evalmeter.Meter
}

func New(cfg *config.Config, logRing *logging.RingBuffer) *App {
	return &App{
		cfg:     cfg,
		sem:     make(chan struct{}, cfg.AcquisitionConcurrency),
		logRing: logRing,
	}
}

func (a *App) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := a.setup(ctx); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port)
		slog.Info("server listening", "addr", addr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	slog.Info("waiting for background tasks")
	a.shutdownComponent(5*time.Second, func(ctx context.Context) {
		if a.alertMonitor != nil {
			a.alertMonitor.Shutdown(ctx)
		}
	})
	a.shutdownComponent(5*time.Second, func(ctx context.Context) {
		if a.eventFeed != nil {
			a.eventFeed.Shutdown(ctx)
		}
	})
	a.shutdownComponent(5*time.Second, func(ctx context.Context) {
		if a.evalMeter != nil {
			a.evalMeter.Shutdown(ctx)
		}
	})
	a.shutdownComponent(10*time.Second, func(ctx context.Context) {
		if a.vocabRefresh != nil {
			a.vocabRefresh.Shutdown(ctx)
		}
	})
	a.shutdownComponent(30*time.Second, func(ctx context.Context) {
		if a.scheduler != nil {
			a.scheduler.Shutdown(ctx)
		}
	})
	// Always drain the shared background group (corpus refresh, metrics rollup —
	// and in-flight acquisitions when the scheduler exists) with a bound. The
	// drain is owned here, not by the scheduler: without it, the no-audio-store
	// path used to block shutdown forever on a bare wg.Wait().
	a.drainBackground(30 * time.Second)

	a.cleanup()
	slog.Info("shutdown complete")
	return nil
}

// shutdownComponent calls fn with a bounded context. Used to give each
// shutdownable component its own timeout without repeating the
// WithTimeout/defer-cancel boilerplate at every call site.
func (a *App) shutdownComponent(timeout time.Duration, fn func(context.Context)) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fn(ctx)
}

// drainBackground waits for every goroutine registered on the App's WaitGroup,
// giving up after the timeout so a hung background task can never wedge shutdown.
func (a *App) drainBackground(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("background task drain timed out")
	}
}

// setup assembles the object graph in dependency order. Each wire* stage owns
// one context's construction; the values crossing contexts travel explicitly
// through the small *Wiring structs.
func (a *App) setup(ctx context.Context) error {
	var err error

	a.pool, err = database.NewPool(ctx, a.cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}

	a.redisClient = sharedRedis.NewClient(ctx, a.cfg.RedisURL)

	verifier, err := authAdapters.NewSupabaseJWTVerifier(
		ctx,
		a.cfg.SupabaseJWTJWKSURL,
		a.cfg.SupabaseProjectURL,
		a.cfg.SupabaseJWTAud,
	)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	a.eventBus = events.NewInProcessBus()
	// Publishers go through the Mission Control tap decorator; subscribers (the
	// per-user SSE stream) read the inner bus directly. Keeps the operator
	// console's vocabulary out of internal/shared/events.
	tap := eventtap.New(a.eventBus)

	disc := a.wireDiscovery(ctx)
	cat := a.wireCatalog(tap, disc.featuredBridge)
	queueHandler := a.wirePlayback(cat.trackRepo)

	r := a.mountRoutes(verifier, cat, queueHandler, disc.handler)
	a.wireAdmin(ctx, r, verifier, tap, disc.requestStore, disc.searchSvc)

	a.startAlertMonitor(ctx)

	a.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return nil
}

// catalogWiring carries the catalog+acquisition values other stages consume:
// the track repository (playback's now-playing reader is built from it) and
// the handlers mountRoutes mounts.
type catalogWiring struct {
	trackRepo       *persistence.PgxTrackRepository
	trackHandler    *catalogHandler.TrackHandler
	playlistHandler *catalogHandler.PlaylistHandler
	streamHandler   *catalogHandler.StreamHandler
	audioURLHandler *catalogHandler.AudioURLHandler
	retryH          *acqHandler.RetryHandler // nil when acquisition is off
}

// wireCatalog builds the catalog context plus the acquisition scheduler that
// backs it (nil-degraded when no audio store is configured).
func (a *App) wireCatalog(tap *eventtap.Tap, featuredBridge *discoverybridge.FeaturedResolver) catalogWiring {
	audioStore := a.buildAudioStore()
	trackRepo := persistence.NewPgxTrackRepository(a.pool)
	playlistRepo := persistence.NewPgxPlaylistRepository(a.pool)

	var audioSearcher acqPorts.AudioSearcher
	if audioStore != nil {
		audioSearcher = ytdlp.NewYtDlpAudioSearcher(a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime)
	}

	var scheduler catalogPorts.AcquisitionScheduler
	if audioSearcher != nil && audioStore != nil {
		audioProber := ytdlp.NewFfprobeProber(a.cfg.FFmpegLocation)
		acquireSvc := acqService.NewAcquireTrackAudioService(
			trackRepo,
			audioSearcher,
			audioStore,
			acqService.WithAcquireEvents(tap),
			acqService.WithAudioProber(audioProber),
			acqService.WithAudioTagger(id3.NewTagger()),
		)
		bgScheduler := acqService.NewBackgroundAcquisitionScheduler(acquireSvc, &a.wg, a.sem,
			acqService.WithSchedulerEvents(tap))
		a.scheduler = bgScheduler
		scheduler = bgScheduler
	}

	addTrackSvc := catalogService.NewAddTrackService(
		trackRepo,
		catalogService.WithAddTrackEvents(tap),
		catalogService.WithAcquisitionScheduler(scheduler),
	)
	listTracksSvc := catalogService.NewListTracksService(trackRepo)
	deleteTrackSvc := catalogService.NewDeleteTrackService(trackRepo, audioStore, catalogService.WithDeleteTrackEvents(tap))
	setTrackNumberSvc := catalogService.NewSetTrackNumberService(trackRepo)
	playlistSvc := catalogService.NewPlaylistService(playlistRepo, trackRepo, catalogService.WithPlaylistEvents(tap))

	backfillFeaturedSvc := catalogService.NewBackfillFeaturedService(trackRepo, featuredBridge)
	listFeaturingSvc := catalogService.NewListFeaturingService(trackRepo)

	trackHandler := catalogHandler.NewTrackHandler(addTrackSvc, listTracksSvc, deleteTrackSvc, setTrackNumberSvc, backfillFeaturedSvc, listFeaturingSvc)
	playlistHandler := catalogHandler.NewPlaylistHandler(playlistSvc)
	streamTrackSvc := catalogService.NewStreamTrackService(trackRepo, audioStore, scheduler)
	streamHandler := catalogHandler.NewStreamHandler(streamTrackSvc)
	audioURLSvc := catalogService.NewAudioURLService(trackRepo, audioStore)
	audioURLHandler := catalogHandler.NewAudioURLHandler(audioURLSvc)

	var retryH *acqHandler.RetryHandler
	if scheduler != nil {
		retryH = acqHandler.NewRetryHandler(trackRepo, scheduler)
	}

	return catalogWiring{
		trackRepo:       trackRepo,
		trackHandler:    trackHandler,
		playlistHandler: playlistHandler,
		streamHandler:   streamHandler,
		audioURLHandler: audioURLHandler,
		retryH:          retryH,
	}
}

// wirePlayback builds the playback context. The now-playing reader is a
// required parameter by design: a miswired graph fails at construction instead
// of silently dropping current_track from every resume.
func (a *App) wirePlayback(trackRepo *persistence.PgxTrackRepository) *playbackHandler.QueueHandler {
	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool)
	nowPlayingReader := catalogbridge.NewNowPlayingReader(trackRepo)
	queueSvc := playbackService.NewQueueService(queueStateRepo, nowPlayingReader)
	return playbackHandler.NewQueueHandler(queueSvc)
}

// discoveryWiring carries the discovery-context values other stages consume:
// the mounted handler, the operator-surface inputs (request store + live search
// service), and the catalog-facing featured-artist bridge.
type discoveryWiring struct {
	handler        *discoveryHandler.DiscoveryHandler
	requestStore   *requeststore.Store
	searchSvc      *discoveryService.Service
	featuredBridge *discoverybridge.FeaturedResolver
}

// wireDiscovery builds the discovery context: search pipeline, content/detail
// services, telemetry-fed background tickers, and the mounted handler.
func (a *App) wireDiscovery(ctx context.Context) discoveryWiring {
	var sharedMB *providers.MusicBrainzAdapter
	if a.cfg.HasMusicBrainz() {
		sharedMB = providers.NewMusicBrainzAdapter(
			newDiscoveryClient(),
			a.cfg.MusicBrainzUserAgent,
		)
	}
	eventStore := discoveryPersistence.NewPgxEventStore(a.pool)
	vocabStore := BuildVocabularyStore(a.redisClient)
	featuredBridge := a.buildFeaturedBridge(sharedMB)
	historySvc, clearHistorySvc := a.buildHistoryServices()
	content := a.buildContentServices(sharedMB, vocabStore)
	enrichSvc := a.buildEnrichmentService(sharedMB)

	requestStore := requeststore.New()
	searchSvc := BuildSearchServiceWithTransport(
		a.cfg,
		a.pool,
		a.redisClient,
		eventStore,
		requeststore.NewTransport(defaultLiveTransport, requestStore),
		false,
	)

	// Behavioral-ranking refresh: when the flag is on, recompute the satisfaction
	// score map off the request path on a ticker. Inert (the option is unset)
	// otherwise. Bound to the app context so it exits on graceful shutdown.
	if a.cfg.BehavioralRankingEnabled {
		searchSvc.StartBehavioralRefresh(ctx, 30*time.Minute)
		slog.Info("behavioral ranking refresh started")
	}

	// Self-growing eval corpus: when a path is configured, nightly-materialize the
	// behavioral labels (search→engagement positives, wrong_album hard negatives)
	// into the eval corpus format. Off when the path is empty.
	a.startCorpusRefresh(ctx, eventStore)

	// Mission Control metrics rollup: persist the daily aggregate gauges so the
	// console's history survives restart. Always on (cheap aggregate upsert).
	a.startMetricsRollup(ctx, discoveryPersistence.NewPgxMetricsRollup(a.pool))

	eventSvc := discoveryService.NewRecordEventService(eventStore)

	discoveryH := discoveryHandler.NewDiscoveryHandler(discoveryHandler.DiscoveryServices{
		Search:       searchSvc,
		History:      historySvc,
		ClearHistory: clearHistorySvc,
		Album:        content.album,
		Artist:       content.artist,
		Related:      content.related,
		Enrich:       enrichSvc,
		Suggest:      content.suggest,
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
		featuredBridge: featuredBridge,
	}
}

// buildFeaturedBridge builds the featured-artist resolver and its catalog
// bridge. The resolver tolerates a nil MB searcher (MusicBrainz not configured)
// and degrades to Deezer-only; a nil interface (not a typed-nil pointer) keeps
// that safe.
func (a *App) buildFeaturedBridge(sharedMB *providers.MusicBrainzAdapter) *discoverybridge.FeaturedResolver {
	featuredDeezer := providers.NewDeezerAdapter(newDiscoveryClient())
	if sharedMB != nil {
		return discoverybridge.NewFeaturedResolver(
			discoveryService.NewFeaturedArtistResolver(sharedMB, featuredDeezer),
		)
	}
	return discoverybridge.NewFeaturedResolver(
		discoveryService.NewFeaturedArtistResolver(nil, featuredDeezer),
	)
}

// buildHistoryServices builds the search-history read and clear services.
func (a *App) buildHistoryServices() (*discoveryService.ListSearchHistoryService, *discoveryService.ClearSearchHistoryService) {
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(a.pool)
	return discoveryService.NewListSearchHistoryService(historyRepo),
		discoveryService.NewClearSearchHistoryService(historyRepo)
}

// contentWiring carries the content-service values wireDiscovery consumes.
type contentWiring struct {
	album   *discoveryService.GetAlbumTracksService
	artist  *discoveryService.GetArtistContentService
	related *discoveryService.GetRelatedTracksService
	suggest *discoveryService.SuggestService
}

// buildContentServices builds the album, artist, related, and suggest services.
func (a *App) buildContentServices(
	sharedMB *providers.MusicBrainzAdapter,
	vocabStore discoveryPorts.VocabularyStore,
) contentWiring {
	deezerContent := providers.NewDeezerAdapter(newDiscoveryClient())
	// iTunes is a second mainstream source of truth for discography/tracklist
	// (docs/providers/itunes.md cap 5): an iTunes-sourced album/artist result
	// carries its collectionId/artistId, which keys the /lookup content endpoint.
	itunesContent := providers.NewITunesAdapter(newDiscoveryClient())

	albumProviders := map[string]discoveryPorts.AlbumContentProvider{
		"deezer": deezerContent,
		"itunes": itunesContent,
	}
	artistProviders := map[string]discoveryPorts.ArtistContentProvider{
		"deezer": deezerContent,
		"itunes": itunesContent,
		// SoundCloud serves the underground long tail: an artist sourced from
		// SoundCloud carries its numeric user id, which keys these endpoints.
		"soundcloud": providers.NewSoundCloudAPIAdapter(newDiscoveryClient(), nil),
	}
	// Last.fm top-tracks, keyed by MBID (identity-safe) — the client calls it only
	// when the artist has a resolved MBID, so it never falls back to ambiguous
	// name matching. Adds the scrobble-popular layer alongside Deezer/SoundCloud.
	if a.cfg.HasLastFM() {
		artistProviders["lastfm"] = providers.NewLastFmAdapter(newDiscoveryClient(), a.cfg.LastFMAPIKey)
	}

	// Related tracks are track-keyed: a SoundCloud-sourced track carries its
	// numeric track id, which keys /tracks/{id}/related. SoundCloud-only today.
	relatedProviders := map[string]discoveryPorts.RelatedTracksProvider{
		"soundcloud": providers.NewSoundCloudAPIAdapter(newDiscoveryClient(), nil),
	}

	// Multi-provider consensus: ALL providers are equal sources, merged into a
	// union. Built via the shared BuildConsensusProviders so coverage signal B
	// measures the same provider set.
	consensusProviders := BuildConsensusProviders(a.cfg, nil)
	var consensusOpts []discoveryService.ConsensusOption
	if sharedMB != nil {
		consensusOpts = append(consensusOpts, discoveryService.WithMBAuthority(sharedMB))
	}
	consensusSvc := discoveryService.NewConsensusService(consensusProviders, consensusOpts...)

	albumSvc := discoveryService.NewGetAlbumTracksService(
		albumProviders,
		discoveryService.WithTrackFeatured(deezerContent),
	)
	artistSvc := discoveryService.NewGetArtistContentService(
		artistProviders,
		discoveryService.WithConsensusService(consensusSvc),
	)
	relatedSvc := discoveryService.NewGetRelatedTracksService(relatedProviders)
	suggestSvc := discoveryService.NewSuggestService(vocabStore)

	return contentWiring{
		album:   albumSvc,
		artist:  artistSvc,
		related: relatedSvc,
		suggest: suggestSvc,
	}
}

// buildEnrichmentService builds the MusicBrainz detail-open enrichment service.
// Returns nil when MusicBrainz is not configured; the handler degrades to an
// empty DTO in that case.
func (a *App) buildEnrichmentService(sharedMB *providers.MusicBrainzAdapter) *discoveryService.EnrichmentService {
	if sharedMB == nil {
		return nil
	}
	enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(a.redisClient)
	return discoveryService.NewEnrichmentService(
		sharedMB,
		buildArtworkChain(clientFactory{}, a.cfg),
		enrichmentCache,
		// Memoize each name resolution so the search path can attach the MBID to
		// a non-MB result later (cap 5 warm).
		discoveryService.WithMBIDMemo(enrichmentCache),
	)
}

// mountRoutes builds the router: middleware, the public health probe, and the
// auth-gated /v1 API. Admin routes are mounted separately by wireAdmin.
func (a *App) mountRoutes(
	verifier auth.TokenVerifier,
	cat catalogWiring,
	queueHandler *playbackHandler.QueueHandler,
	discoveryH *discoveryHandler.DiscoveryHandler,
) *chi.Mux {
	r := chi.NewRouter()

	r.Use(httputil.CorrelationID)
	r.Use(httputil.Recoverer)
	r.Use(httputil.RequestLogger)
	r.Use(httputil.MaxBodySize(1 << 20)) // 1MB limit on request bodies
	corsHeaders := []string{"Accept", "Authorization", "Content-Type"}
	if a.cfg.IsDevelopment() {
		corsHeaders = append(corsHeaders, "ngrok-skip-browser-warning")
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   a.cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   corsHeaders,
		ExposedHeaders:   []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", a.handleHealth)

	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.Middleware(verifier))

		r.Mount("/tracks", cat.trackHandler.Routes())
		r.Get("/tracks/{trackId}/audio", cat.streamHandler.HandleStreamAudio)
		r.Post("/tracks/{trackId}/audio/recover", cat.streamHandler.HandleRecover)
		r.Post("/audio-urls", cat.audioURLHandler.HandleResolve)
		if cat.retryH != nil {
			r.Post("/tracks/{trackId}/retry", cat.retryH.HandleRetryAcquisition)
		}
		r.Mount("/playlists", cat.playlistHandler.Routes())
		r.Mount("/playback", queueHandler.Routes())
		r.Mount("/discovery", discoveryH.Routes())
		r.Handle("/events", &sseHandler{bus: a.eventBus})
	})

	return r
}

// wireAdmin builds the Mission Control operator console — two-layer gate: auth
// first, then the operator-only check inside adminH's data routes. Fails closed
// when OperatorUserID is unset.
func (a *App) wireAdmin(
	ctx context.Context,
	r *chi.Mux,
	verifier auth.TokenVerifier,
	tap *eventtap.Tap,
	requestStore *requeststore.Store,
	searchSvc *discoveryService.Service,
) {
	a.eventFeed = eventtap.NewFeed()
	a.eventFeed.Start(ctx, tap)
	var acqReader adminHandler.AcquisitionStatusReader
	if a.scheduler != nil {
		acqReader = a.scheduler
	}

	// AIDEV-NOTE: eval meter ships OFF by default (EVAL_METER_ENABLED). The runner
	// runs a tiny fixed smoke-query set through a *dedicated* search-service
	// instance whose per-provider circuit breakers are isolated from production's,
	// so eval failures can't trip the breakers live search depends on. When
	// disabled, buildEvalRunner returns nil and no second provider stack is built.
	a.evalMeter = evalmeter.New(a.cfg.EvalMeterEnabled, 0, a.buildEvalRunner())
	a.evalMeter.Start(ctx)
	adminH := adminHandler.New(a.dependencyHealth, a.logRing).
		WithSupabaseLogin(a.cfg.SupabaseProjectURL, a.cfg.SupabaseAnonKey).
		WithEventFeed(a.eventFeed).
		WithProviderHealth(a.providerHealth).
		WithAcquisition(acqReader).
		WithEvalMeter(a.evalMeter).
		WithRequestStore(requestStore).
		WithReRunner(a.buildReRunner(searchSvc)).
		WithSearchInspector(a.buildSearchInspector(searchSvc))
	r.Route("/admin", func(ar chi.Router) {
		ar.Get("/", adminH.ServeIndex)        // public shell — holds no data
		ar.Get("/config", adminH.ServeConfig) // public client config for sign-in
		ar.Group(func(gr chi.Router) {        // gated data: auth, then operator check
			gr.Use(auth.Middleware(verifier))
			gr.Use(adminHandler.OperatorOnly(a.cfg.OperatorUserID))
			adminH.RegisterData(gr)
		})
	})
}

// handleHealth is the public readiness probe. It deliberately exposes no
// dependency topology: just whether the service can serve. The detailed
// per-dependency breakdown lives behind the operator-gated /admin/health tile.
func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	if a.dependencyHealth(r.Context()).Healthy() {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded"})
}

// dependencyHealth pings the configured dependencies and reports each one's
// status. A nil dependency is "not_configured" (intentionally absent), distinct
// from "down" (configured but unreachable).
func (a *App) dependencyHealth(ctx context.Context) adminHandler.DependencyHealth {
	detail := adminHandler.DependencyDetail{CheckedAt: time.Now().UTC()}

	dbStatus := "ok"
	if a.pool == nil {
		dbStatus = "not_configured"
	} else {
		start := time.Now()
		if !database.CheckHealth(ctx, a.pool).OK {
			dbStatus = "down"
			detail.DBError = "health check failed"
		}
		detail.DBLatencyMs = time.Since(start).Milliseconds()
	}

	redisStatus := "ok"
	if a.redisClient == nil {
		redisStatus = "not_configured"
	} else {
		start := time.Now()
		if err := a.redisClient.Ping(ctx).Err(); err != nil {
			redisStatus = "down"
			detail.RedisError = err.Error()
		}
		detail.RedisLatencyMs = time.Since(start).Milliseconds()
	}

	return adminHandler.DependencyHealth{DB: dbStatus, Redis: redisStatus, Detail: detail}
}

// startAlertMonitor builds and starts the Mission Control alert monitor. It
// pages the operator on Signal conditions (dependency down to start), pushing
// via ntfy when configured and logging only otherwise. Alert messages carry
// only state names, never connection details.
func (a *App) startAlertMonitor(ctx context.Context) {
	var notifier adminAlert.AlertNotifier = adminAlert.NopNotifier{}
	if a.cfg.HasAlertPush() {
		notifier = adminAlert.NewNtfyNotifier(a.cfg.AlertNtfyURL)
	}

	dependencyDown := adminAlert.Condition{
		Key: "dependency_down",
		Eval: func(ctx context.Context) *adminAlert.Alert {
			h := a.dependencyHealth(ctx)
			if h.Healthy() {
				return nil
			}
			msg := "dependencies down:"
			if h.DB == "down" {
				msg += " db"
			}
			if h.Redis == "down" {
				msg += " redis"
			}
			return &adminAlert.Alert{
				Title:    "altune dependency down",
				Message:  msg,
				Severity: adminAlert.SeveritySignal,
			}
		},
	}

	conditions := []adminAlert.Condition{dependencyDown}

	// Coverage alert: page when zero-result searches in the last 24h exceed the
	// threshold — the computed-but-unwatched coverage signal now pages the operator
	// instead of waiting to be noticed. Disabled when the threshold is 0. The
	// message carries the aggregate count only — never the query text or any user
	// id (cardinality / privacy).
	if a.cfg.AlertZeroResultThreshold > 0 {
		conditions = append(conditions, buildCoverageCondition(a.pool, a.cfg.AlertZeroResultThreshold))
	}

	a.alertMonitor = adminAlert.NewMonitor(notifier, 30*time.Second, conditions...)
	a.alertMonitor.Start(ctx)
}

// buildCoverageCondition returns an alert condition that pages when zero-result
// searches in the last 24h exceed threshold.
func buildCoverageCondition(pool *pgxpool.Pool, threshold int) adminAlert.Condition {
	eventQuery := discoveryPersistence.NewPgxEventStore(pool)
	return adminAlert.Condition{
		Key: "coverage_zero_result",
		Eval: func(ctx context.Context) *adminAlert.Alert {
			since := time.Now().UTC().Add(-24 * time.Hour)
			rows, err := eventQuery.ZeroResultQueries(ctx, since, 1000)
			if err != nil {
				slog.WarnContext(ctx, "coverage alert query failed", "error", err)
				return nil
			}
			total := 0
			for _, r := range rows {
				total += r.Count
			}
			if total < threshold {
				return nil
			}
			return &adminAlert.Alert{
				Title:    "altune discovery coverage gap",
				Message:  fmt.Sprintf("zero-result searches in 24h: %d (threshold %d)", total, threshold),
				Severity: adminAlert.SeveritySignal,
			}
		},
	}
}

func (a *App) buildAudioStore() catalogPorts.AudioStore {
	if a.cfg.HasOCIS3() {
		store, err := storage.NewObjectStorageAudioStore(
			a.cfg.OCIS3Endpoint,
			a.cfg.OCIS3AccessKey,
			a.cfg.OCIS3SecretKey,
			a.cfg.OCIS3Bucket,
			a.cfg.OCIS3Region,
		)
		if err != nil {
			slog.Warn("OCI S3 store failed to initialize, falling back", "error", err)
		} else {
			slog.Info("audio store: OCI Object Storage")
			return store
		}
	}

	if a.cfg.MusicDir != "" {
		slog.Info("audio store: filesystem", "dir", a.cfg.MusicDir)
		return storage.NewFilesystemAudioStore(a.cfg.MusicDir)
	}

	slog.Warn("no audio store configured")
	return nil
}

// buildDetailEnrichers wires the optional, provider-keyed detail-open enrichers
// (Discogs album+artist, Last.fm, Deezer, lyrics) into one DetailEnrichers
// bundle. Each is best-effort: an unconfigured provider leaves its field nil and
// the endpoint answers an empty DTO. The MusicBrainz enricher is deliberately
// NOT here — it also feeds the search path, so it stays wired in setup.
func (a *App) buildDetailEnrichers() discoveryHandler.DetailEnrichers {
	var enrichers discoveryHandler.DetailEnrichers

	// Discogs album + artist: credits/personnel, styles, label/catalog, companies,
	// community signal + artist bio/aliases/links (docs/providers/discogs.md caps
	// 3–7). Only when a Discogs token is configured.
	if a.cfg.HasDiscogs() {
		discogsAdapter := providers.NewDiscogsAdapter(
			newDiscoveryClient(),
			a.cfg.DiscogsToken,
			a.cfg.MusicBrainzUserAgent,
		)
		enrichers.Discogs = discoveryEnrich.NewDiscogsEnrichmentService(
			discogsAdapter,
			discoveryCacheAdapters.NewRedisDiscogsEnrichmentCache(a.redisClient),
		)
		enrichers.DiscogsArtist = discoveryEnrich.NewDiscogsArtistEnrichmentService(
			discogsAdapter,
			discoveryCacheAdapters.NewRedisDiscogsArtistEnrichmentCache(a.redisClient),
		)
	}

	// Last.fm: listen-based popularity, weighted tags, bio, similar-artist graph,
	// MBID bridge (docs/providers/lastfm.md cap 3). Only when a Last.fm key is set.
	if a.cfg.HasLastFM() {
		lfmEnricher := providers.NewLastFmAdapter(newDiscoveryClient(), a.cfg.LastFMAPIKey)
		enrichers.LastFm = discoveryEnrich.NewLastFmEnrichmentService(
			lfmEnricher,
			discoveryCacheAdapters.NewRedisLastFmEnrichmentCache(a.redisClient),
		)
	}

	// Deezer: track audio fields (bpm/gain) + explicit flag, album liner data
	// (docs/providers/deezer.md caps 7–8). Public API, no key — wired always.
	enrichers.Deezer = discoveryEnrich.NewDeezerEnrichmentService(
		providers.NewDeezerAdapter(newDiscoveryClient()),
		discoveryCacheAdapters.NewRedisDeezerEnrichmentCache(a.redisClient),
	)

	// Deezer lyrics: synced + plain lyrics, writers, copyright — the one axis no
	// other audited provider carries (docs/providers/deezer.md cap 6). Via the
	// pipe.deezer.com GraphQL (anonymous-JWT, self-healing); no key — wired always.
	enrichers.Lyrics = discoveryEnrich.NewLyricsService(
		providers.NewDeezerLyricsAdapter(newDiscoveryClient()),
		discoveryCacheAdapters.NewRedisDeezerLyricsCache(a.redisClient),
	)

	return enrichers
}

func buildDiscoveryProviders(cf clientFactory, cfg *config.Config, mb *providers.MusicBrainzAdapter) []discoveryPorts.SearchProvider {
	var providerList []discoveryPorts.SearchProvider

	deezerClient := cf.discovery()
	providerList = append(providerList, providers.NewDeezerAdapter(deezerClient))

	itunesClient := cf.discovery()
	providerList = append(providerList, providers.NewITunesAdapter(itunesClient))

	// TheAudioDB is intentionally NOT a search provider: its free key caps artist
	// search at 1 result and it carries no ranking signal, so it fails the
	// ambiguous-query case while Deezer/MB/Last.fm/YT already cover artists. It is
	// kept as an artwork-by-identity resolver in buildArtworkChain. (audit §3.8)

	if mb != nil {
		providerList = append(providerList, mb)
	}

	if cfg.HasLastFM() {
		lfmClient := cf.discovery()
		providerList = append(providerList, providers.NewLastFmAdapter(lfmClient, cfg.LastFMAPIKey))
	}

	// Direct api-v2 SoundCloud client (coverage: unreleased/underground long tail),
	// with the yt-dlp adapter as fallback when client_id resolution is down.
	soundcloudClient := cf.discovery()
	providerList = append(providerList, providers.NewSoundCloudAPIAdapter(
		soundcloudClient,
		providers.NewSoundCloudAdapter(),
	))

	providerList = append(providerList, providers.NewYouTubeMusicAdapter(cf.roundTripper()))

	slog.Info("discovery providers configured", "count", len(providerList))
	return providerList
}

// startCorpusRefresh runs the nightly self-growing-corpus materialization when
// BEHAVIORAL_CORPUS_PATH is set: it mines the last 30 days of behavioral labels
// and writes them to the configured path in the eval corpus format. Best-effort —
// a failure is logged, never fatal. Exits when ctx is cancelled (graceful
// shutdown). A no-op when the path is empty.
func (a *App) startCorpusRefresh(ctx context.Context, store discoveryPorts.BehavioralLabelStore) {
	if a.cfg.BehavioralCorpusPath == "" {
		return
	}
	builder := eval.NewCorpusBuilder(store)
	const lookback = 30 * 24 * time.Hour
	a.startTicker(ctx, 24*time.Hour, func() {
		since := time.Now().UTC().Add(-lookback)
		if err := builder.Materialize(ctx, since, since.Format("2006-01-02"), a.cfg.BehavioralCorpusPath); err != nil {
			slog.WarnContext(ctx, "behavioral corpus materialize failed", "error", err)
			return
		}
		slog.InfoContext(ctx, "behavioral corpus materialized", "path", a.cfg.BehavioralCorpusPath)
	})
	slog.Info("behavioral corpus refresh started", "path", a.cfg.BehavioralCorpusPath)
}

// startMetricsRollup rolls up today's (and yesterday's, for late-arriving
// events) Mission Control gauges every 6 hours, persisting them to
// discovery_metrics so the console's week-over-week history survives restart.
// Best-effort; bound to the app context for graceful shutdown.
func (a *App) startMetricsRollup(ctx context.Context, store discoveryPorts.MetricsRollupStore) {
	a.startTicker(ctx, 6*time.Hour, func() {
		now := time.Now().UTC()
		for _, day := range []time.Time{now, now.Add(-24 * time.Hour)} {
			if err := store.RollupDay(ctx, day); err != nil {
				slog.WarnContext(ctx, "discovery metrics rollup failed", "error", err)
			}
		}
	})
	slog.Info("discovery metrics rollup started")
}

// startTicker runs fn immediately, then on every interval until ctx is
// cancelled. The goroutine is tracked on the App's WaitGroup.
func (a *App) startTicker(ctx context.Context, interval time.Duration, fn func()) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		fn()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fn()
			}
		}
	}()
}

func (a *App) startVocabularyRefresh(vocabStore discoveryPorts.VocabularyStore) {
	if vocabStore == nil {
		return
	}
	charts := a.buildChartProviders()
	if len(charts) == 0 {
		return
	}
	a.vocabRefresh = discoveryService.NewVocabularyRefreshService(
		charts, vocabStore, 6*time.Hour, 50,
	)
	a.vocabRefresh.Start()
	slog.Info("vocabulary refresh started")
}

func (a *App) buildChartProviders() []discoveryPorts.ChartProvider {
	var charts []discoveryPorts.ChartProvider
	deezerClient := newChartClient()
	charts = append(charts, providers.NewDeezerAdapter(deezerClient))
	if a.cfg.HasLastFM() {
		lfmClient := newChartClient()
		charts = append(charts, providers.NewLastFmAdapter(
			lfmClient, a.cfg.LastFMAPIKey,
		))
	}
	return charts
}

func (a *App) cleanup() {
	if a.pool != nil {
		a.pool.Close()
	}
	if a.redisClient != nil {
		a.redisClient.Close()
	}
}
