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
	discoveryCatalogBridge "altune/go-api/internal/discovery/adapters/catalogbridge"
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
	"altune/go-api/internal/shared/leader"
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

	election         *leader.Election
	backgroundStarts []func(context.Context)
}

const backgroundLockKey int64 = 8_246_113_907_441_002

func (a *App) whenLeader(start func(context.Context)) {
	a.backgroundStarts = append(a.backgroundStarts, start)
}

func (a *App) startBackgroundWhenLeader(ctx context.Context) {
	a.election = leader.NewElection(a.pool, backgroundLockKey)
	a.election.Start(ctx)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if !a.election.Await(ctx) {
			return
		}
		for _, start := range a.backgroundStarts {
			start(ctx)
		}
	}()
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
	a.shutdownComponent(5*time.Second, func(ctx context.Context) {
		if a.election != nil {
			a.election.Shutdown(ctx)
		}
	})
	a.drainBackground(30 * time.Second)

	a.cleanup()
	slog.Info("shutdown complete")
	return nil
}

func (a *App) shutdownComponent(timeout time.Duration, fn func(context.Context)) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fn(ctx)
}

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
	tap := eventtap.New(a.eventBus)

	disc := a.wireDiscovery(ctx)
	cat := a.wireCatalog(tap, disc.featuredBridge)
	queueHandler := a.wirePlayback(cat.trackRepo)
	disc.handler.
		WithOwnership(discoveryCatalogBridge.NewOwnershipReader(cat.trackRepo)).
		WithTrackNumberFiller(discoveryCatalogBridge.NewTrackNumberWriter(cat.setTrackNumberSvc))

	r := a.mountRoutes(verifier, cat, queueHandler, disc.handler)
	a.wireAdmin(ctx, r, verifier, tap, disc.requestStore, disc.searchSvc, disc.artistSvc)

	a.startAlertMonitor(ctx)
	a.startBackgroundWhenLeader(ctx)

	a.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return nil
}

type catalogWiring struct {
	trackRepo         *persistence.PgxTrackRepository
	setTrackNumberSvc *catalogService.SetTrackNumberService
	trackHandler      *catalogHandler.TrackHandler
	libraryHandler    *catalogHandler.LibraryHandler
	playlistHandler   *catalogHandler.PlaylistHandler
	streamHandler     *catalogHandler.StreamHandler
	audioURLHandler   *catalogHandler.AudioURLHandler
	retryH            *acqHandler.RetryHandler
}

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
	playlistLifecycleSvc := catalogService.NewPlaylistLifecycleService(playlistRepo, catalogService.WithPlaylistLifecycleEvents(tap))
	playlistMembershipSvc := catalogService.NewPlaylistMembershipService(playlistRepo, trackRepo, catalogService.WithPlaylistMembershipEvents(tap))

	backfillFeaturedSvc := catalogService.NewBackfillFeaturedService(trackRepo, featuredBridge)
	listFeaturingSvc := catalogService.NewListFeaturingService(trackRepo)

	getTrackStatusSvc := catalogService.NewGetTrackStatusService(trackRepo)
	featuredArtistHandler := catalogHandler.NewFeaturedArtistHandler(backfillFeaturedSvc, listFeaturingSvc)
	trackHandler := catalogHandler.NewTrackHandler(addTrackSvc, listTracksSvc, getTrackStatusSvc, deleteTrackSvc, setTrackNumberSvc, featuredArtistHandler)
	playlistHandler := catalogHandler.NewPlaylistHandler(playlistLifecycleSvc, playlistMembershipSvc)
	streamTrackSvc := catalogService.NewStreamTrackService(trackRepo, audioStore, catalogService.WithStreamScheduler(scheduler))
	streamHandler := catalogHandler.NewStreamHandler(streamTrackSvc)
	audioURLSvc := catalogService.NewAudioURLService(trackRepo, audioStore)
	audioURLHandler := catalogHandler.NewAudioURLHandler(audioURLSvc)

	var retryH *acqHandler.RetryHandler
	if scheduler != nil {
		retryH = acqHandler.NewRetryHandler(trackRepo, scheduler)
	}

	return catalogWiring{
		trackRepo:         trackRepo,
		setTrackNumberSvc: setTrackNumberSvc,
		trackHandler:      trackHandler,
		libraryHandler:    catalogHandler.NewLibraryHandler(catalogService.NewLibraryLensService(trackRepo)),
		playlistHandler:   playlistHandler,
		streamHandler:     streamHandler,
		audioURLHandler:   audioURLHandler,
		retryH:            retryH,
	}
}

func (a *App) wirePlayback(trackRepo *persistence.PgxTrackRepository) *playbackHandler.QueueHandler {
	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool)
	nowPlayingReader := catalogbridge.NewNowPlayingReader(trackRepo)
	queueSvc := playbackService.NewQueueService(queueStateRepo, nowPlayingReader)
	return playbackHandler.NewQueueHandler(queueSvc)
}

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
	r.Use(httputil.MaxBodySize(1 << 20))
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

		r.Route("/tracks", func(r chi.Router) {
			r.Mount("/", cat.trackHandler.Routes())
		})
		r.Get("/tracks/{trackId}/audio", cat.streamHandler.HandleStreamAudio)
		r.Post("/tracks/{trackId}/audio/recover", cat.streamHandler.HandleRecover)
		r.Post("/audio-urls", cat.audioURLHandler.HandleResolve)
		if cat.retryH != nil {
			r.Post("/tracks/{trackId}/retry", cat.retryH.HandleRetryAcquisition)
		}
		r.Mount("/library", cat.libraryHandler.Routes())
		r.Mount("/playlists", cat.playlistHandler.Routes())
		r.Mount("/playback", queueHandler.Routes())
		r.Mount("/discovery", discoveryH.Routes())
		r.Handle("/events", &sseHandler{bus: a.eventBus})
	})

	return r
}

func (a *App) wireAdmin(
	ctx context.Context,
	r *chi.Mux,
	verifier auth.TokenVerifier,
	tap *eventtap.Tap,
	requestStore *requeststore.Store,
	searchSvc *discoveryService.Service,
	artistSvc *discoveryService.GetArtistContentService,
) {
	a.eventFeed = eventtap.NewFeed()
	a.eventFeed.Start(ctx, tap)
	var acqReader adminHandler.AcquisitionStatusReader
	if a.scheduler != nil {
		acqReader = a.scheduler
	}

	a.evalMeter = evalmeter.New(a.cfg.EvalMeterEnabled, 0, a.buildEvalRunner())
	a.whenLeader(a.evalMeter.Start)
	adminH := adminHandler.New(a.dependencyHealth, a.logRing).
		WithSupabaseLogin(a.cfg.SupabaseProjectURL, a.cfg.SupabaseAnonKey).
		WithEventFeed(a.eventFeed).
		WithProviderHealth(a.providerHealth).
		WithAcquisition(acqReader).
		WithEvalMeter(a.evalMeter).
		WithRequestStore(requestStore).
		WithReRunner(a.buildReRunner(searchSvc)).
		WithSearchInspector(a.buildSearchInspector(searchSvc)).
		WithDetailReRunner(a.buildDetailReRunner(searchSvc, artistSvc)).
		WithMetricsHistory(discoveryPersistence.NewPgxMetricsRollup(a.pool))
	r.Route("/admin", func(ar chi.Router) {
		ar.Get("/", adminH.ServeIndex)
		ar.Get("/config", adminH.ServeConfig)
		ar.Group(func(gr chi.Router) {
			gr.Use(auth.Middleware(verifier))
			gr.Use(adminHandler.OperatorOnly(a.cfg.OperatorUserID))
			adminH.RegisterData(gr)
		})
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	if a.dependencyHealth(r.Context()).Healthy() {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded"})
}

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

	if a.cfg.AlertZeroResultThreshold > 0 {
		conditions = append(conditions, buildCoverageCondition(a.pool, a.cfg.AlertZeroResultThreshold))
	}

	a.alertMonitor = adminAlert.NewMonitor(notifier, 30*time.Second, conditions...)
	a.whenLeader(a.alertMonitor.Start)
}

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

func (a *App) buildDetailEnrichers() discoveryHandler.DetailEnrichers {
	var enrichers discoveryHandler.DetailEnrichers

	if a.cfg.HasLastFM() {
		lfmEnricher := providers.NewLastFmAdapter(newDiscoveryClient(), a.cfg.LastFMAPIKey)
		enrichers.LastFm = discoveryEnrich.NewLastFmEnrichmentService(
			lfmEnricher,
			discoveryCacheAdapters.NewRedisLastFmEnrichmentCache(a.redisClient),
		)
	}

	enrichers.Deezer = discoveryEnrich.NewDeezerEnrichmentService(
		providers.NewDeezerAdapter(newDiscoveryClient()),
		discoveryCacheAdapters.NewRedisDeezerEnrichmentCache(a.redisClient),
	)

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

	appleMusicClient := cf.discovery()
	providerList = append(providerList, providers.NewAppleMusicAdapter(appleMusicClient))

	if mb != nil {
		providerList = append(providerList, mb)
	}

	if cfg.HasLastFM() {
		lfmClient := cf.discovery()
		providerList = append(providerList, providers.NewLastFmAdapter(lfmClient, cfg.LastFMAPIKey))
	}

	soundcloudClient := cf.discovery()
	providerList = append(providerList, providers.NewSoundCloudAPIAdapter(
		soundcloudClient,
		providers.NewSoundCloudAdapter(),
	))

	providerList = append(providerList, providers.NewYouTubeMusicAdapter(cf.roundTripper()))

	amazonClient := cf.discovery()
	providerList = append(providerList, providers.NewAmazonMusicAdapter(amazonClient))

	spotifyClient := cf.discovery()
	providerList = append(providerList, providers.NewSpotifyAdapter(spotifyClient))

	slog.Info("discovery providers configured", "count", len(providerList))
	return providerList
}

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

func (a *App) startTicker(ctx context.Context, interval time.Duration, fn func()) {
	a.whenLeader(func(ctx context.Context) { a.runTicker(ctx, interval, fn) })
}

func (a *App) runTicker(ctx context.Context, interval time.Duration, fn func()) {
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
	a.whenLeader(func(context.Context) {
		a.vocabRefresh.Start()
		slog.Info("vocabulary refresh started")
	})
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
