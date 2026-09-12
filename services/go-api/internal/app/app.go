package app

import (
	"altune/go-api/internal/acquisition/adapters/chromaprint"
	"altune/go-api/internal/acquisition/adapters/id3"
	"altune/go-api/internal/acquisition/adapters/streamrip"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/adapters/discoverybridge"
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/discovery/service/eval"
	"altune/go-api/internal/playback/adapters/catalogbridge"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	acqDiscoveryBridge "altune/go-api/internal/acquisition/adapters/discoverybridge"
	acqHandler "altune/go-api/internal/acquisition/adapters/handler"

	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"
	adminAlert "altune/go-api/internal/admin/alert"

	adminHandler "altune/go-api/internal/admin/handler"

	authProviders "altune/go-api/internal/auth/adapters/providers"

	catalogHandler "altune/go-api/internal/catalog/adapters/handler"
	catalogMetrics "altune/go-api/internal/catalog/adapters/metrics"

	catalogPorts "altune/go-api/internal/catalog/ports"
	catalogService "altune/go-api/internal/catalog/service"
	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryCatalogBridge "altune/go-api/internal/discovery/adapters/catalogbridge"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"

	feedbackGithub "altune/go-api/internal/feedback/adapters/github"
	feedbackHandler "altune/go-api/internal/feedback/adapters/handler"
	feedbackService "altune/go-api/internal/feedback/service"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackService "altune/go-api/internal/playback/service"

	sharedRedis "altune/go-api/internal/shared/redis"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type App struct {
	cfg             *config.Config
	pool            *pgxpool.Pool
	dbHealth        dbHealthChecker
	depProbeTimeout time.Duration
	redisClient     *goredis.Client
	authVerifier    authHealthChecker
	server          *http.Server
	wg              sync.WaitGroup
	sem             chan struct{}
	scheduler       *acqService.BackgroundAcquisitionScheduler
	vocabRefresh    *discoveryService.VocabularyRefreshService
	searchSvc       *discoveryService.Service
	eventBus        *events.InProcessBus
	alertMonitor    *adminAlert.Monitor
	logRing         *logging.RingBuffer
	eventFeed       *eventtap.Feed
	providerHealth  *providerhealth.Store
	evalMeter       *evalmeter.Meter

	election         *leader.Election
	backgroundStarts []backgroundJob
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
	outcomes := []shutdownOutcome{
		a.shutdownComponent("alert monitor", 5*time.Second, func(ctx context.Context) {
			if a.alertMonitor != nil {
				a.alertMonitor.Shutdown(ctx)
			}
		}),
		a.shutdownComponent("event feed", 5*time.Second, func(ctx context.Context) {
			if a.eventFeed != nil {
				a.eventFeed.Shutdown(ctx)
			}
		}),
		a.shutdownComponent("eval meter", 5*time.Second, func(ctx context.Context) {
			if a.evalMeter != nil {
				a.evalMeter.Shutdown(ctx)
			}
		}),
		a.shutdownComponent("vocabulary refresh", 10*time.Second, func(ctx context.Context) {
			if a.vocabRefresh != nil {
				a.vocabRefresh.Shutdown(ctx)
			}
		}),
		a.shutdownComponent("acquisition scheduler", 30*time.Second, func(ctx context.Context) {
			if a.scheduler != nil {
				a.scheduler.Shutdown(ctx)
			}
		}),
		a.shutdownComponent("leader election", 5*time.Second, func(ctx context.Context) {
			if a.election != nil {
				a.election.Shutdown(ctx)
			}
		}),
		a.drainBackground(30 * time.Second),
		a.drainSearchBackground(30 * time.Second),
	}

	if unstopped := unfinishedShutdowns(outcomes); len(unstopped) > 0 {
		// These components blew past their shutdown budget and are presumed
		// still running. cleanup() is about to close the DB pool and Redis
		// client out from under them, so name them loudly first.
		slog.Warn("closing DB/Redis while components are still shutting down",
			"components", strings.Join(unstopped, ", "))
	}

	a.cleanup()
	slog.Info("shutdown complete")
	return nil
}

// shutdownOutcome records whether one component's bounded shutdown finished
// within its budget. A component that timed out is presumed still running when
// cleanup() closes the DB pool and Redis client, so the distinction must be
// surfaced rather than swallowed.
type shutdownOutcome struct {
	name      string
	completed bool
}

// unfinishedShutdowns returns the names of components that did not complete
// shutdown within their budget, in declaration order.
func unfinishedShutdowns(outcomes []shutdownOutcome) []string {
	var names []string
	for _, o := range outcomes {
		if !o.completed {
			names = append(names, o.name)
		}
	}
	return names
}

// shutdownComponent runs fn with a bounded context and reports whether it
// returned before the budget elapsed. fn runs on its own goroutine so a
// component that ignores the deadline cannot wedge the whole shutdown sequence;
// a timeout is surfaced as an outcome (and logged) instead of silently falling
// through to cleanup().
func (a *App) shutdownComponent(name string, timeout time.Duration, fn func(context.Context)) shutdownOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(ctx)
	}()
	select {
	case <-done:
		return shutdownOutcome{name: name, completed: true}
	case <-ctx.Done():
		slog.Warn("component shutdown exceeded its budget",
			"component", name, "timeout", timeout.String())
		return shutdownOutcome{name: name, completed: false}
	}
}

func (a *App) setup(ctx context.Context) error {
	var err error

	a.pool, err = database.NewPool(ctx, a.cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	a.dbHealth = func(ctx context.Context) database.HealthStatus {
		return database.CheckHealth(ctx, a.pool)
	}

	a.redisClient = sharedRedis.NewClient(ctx, a.cfg.RedisURL)

	verifier, err := authProviders.NewSupabaseJWTVerifier(
		ctx,
		a.cfg.SupabaseJWTJWKSURL,
		a.cfg.SupabaseProjectURL,
		a.cfg.SupabaseJWTAud,
	)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	a.authVerifier = verifier

	a.eventBus = events.NewInProcessBus()
	tap := eventtap.New(a.eventBus)

	disc := a.wireDiscovery(ctx)
	cat, err := a.wireCatalog(tap, disc.featuredBridge, disc.searchSvc)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	queueHandler := a.wirePlayback(cat.trackRepo)
	disc.handler.
		WithOwnership(discoveryCatalogBridge.NewOwnershipReader(cat.trackRepo)).
		WithTrackNumberFiller(discoveryCatalogBridge.NewTrackNumberWriter(cat.setTrackNumberSvc))

	r := a.mountRoutes(verifier, cat, queueHandler, disc.handler, a.wireFeedback())
	a.wireAdmin(ctx, r, verifier, tap, disc.requestStore, disc.searchSvc, disc.artistSvc)

	a.startAlertMonitor(ctx)
	a.startStalePendingReconcile(ctx, cat.trackRepo)
	a.startBackgroundWhenLeader(ctx)

	a.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port),
		Handler: r,
		// Tie every request context to the app lifecycle context so that
		// server.Shutdown cancels long-lived streaming handlers (SSE) instead
		// of blocking on them until the shutdown timeout elapses.
		BaseContext:       func(net.Listener) context.Context { return ctx },
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
	reacquireH        *acqHandler.ReacquireHandler
}

func (a *App) wireCatalog(
	tap *eventtap.Tap,
	featuredBridge *discoverybridge.FeaturedResolver,
	searchSvc *discoveryService.Service,
) (catalogWiring, error) {
	audioStore, err := a.buildAudioStore()
	if err != nil {
		return catalogWiring{}, err
	}
	trackRepo := persistence.NewPgxTrackRepository(a.pool)
	catalogTrackRepo := persistence.NewPgxCatalogTrackRepository(a.pool)
	playlistRepo := persistence.NewPgxPlaylistRepository(a.pool)

	var audioSources []acqPorts.AudioSource
	if audioStore != nil {
		searcher := ytdlp.NewYtDlpAudioSearcher(
			a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime)
		audioSources = append(audioSources, ytmusic.NewSource(searcher))
		audioSources = append(audioSources, a.buildStreamripSources()...)
		audioSources = append(audioSources, ytdlp.NewSource(searcher))
	}

	var scheduler catalogPorts.AcquisitionScheduler
	if len(audioSources) > 0 && audioStore != nil {
		audioProber := ytdlp.NewFfprobeProber(a.cfg.FFmpegLocation)
		ffprobeOK, ffmpegOK := audioProber.Available()
		verification := acqService.AcquisitionVerification{Ffprobe: ffprobeOK, Ffmpeg: ffmpegOK}

		acquireOpts := []func(*acqService.AcquireTrackAudioService){
			acqService.WithAcquireEvents(tap),
			acqService.WithAudioProber(audioProber),
			acqService.WithAudioTagger(id3.NewTagger()),
		}
		if searchSvc != nil {
			acquireOpts = append(acquireOpts, acqService.WithRecordingResolver(
				acqDiscoveryBridge.NewRecordingResolver(searchSvc)))
		}
		if a.cfg.AcoustIDAPIKey != "" {
			identifier := chromaprint.NewIdentifier(a.cfg.FFmpegLocation, a.cfg.AcoustIDAPIKey)
			verification.Fpcalc = identifier.Available()
			acquireOpts = append(acquireOpts, acqService.WithAudioIdentifier(identifier))
			slog.Info("acquisition: fingerprint verification enabled", "fpcalc", verification.Fpcalc)
		}
		acquireSvc := acqService.NewAcquireTrackAudioService(
			trackRepo,
			acqService.NewSourceRegistry(audioSources...),
			audioStore,
			acquireOpts...,
		)
		bgScheduler := acqService.NewBackgroundAcquisitionScheduler(acquireSvc, &a.wg, a.sem,
			acqService.WithSchedulerEvents(tap),
			acqService.WithVerificationStatus(verification))
		a.scheduler = bgScheduler
		scheduler = bgScheduler
	}

	addTrackSvc := catalogService.NewAddTrackService(
		catalogTrackRepo,
		catalogService.WithAddTrackEvents(tap),
		catalogService.WithAcquisitionScheduler(scheduler),
	)
	listTracksSvc := catalogService.NewListTracksService(catalogTrackRepo)
	audioStoreMetrics := catalogMetrics.NewExpvarAudioStoreMetrics()
	deleteTrackSvc := catalogService.NewDeleteTrackService(catalogTrackRepo, audioStore, catalogService.WithDeleteTrackEvents(tap), catalogService.WithDeleteTrackMetrics(audioStoreMetrics))
	setTrackNumberSvc := catalogService.NewSetTrackNumberService(catalogTrackRepo)
	playlistLifecycleSvc := catalogService.NewPlaylistLifecycleService(playlistRepo, catalogService.WithPlaylistLifecycleEvents(tap))
	playlistMembershipSvc := catalogService.NewPlaylistMembershipService(playlistRepo, catalogTrackRepo, catalogService.WithPlaylistMembershipEvents(tap))

	backfillFeaturedSvc := catalogService.NewBackfillFeaturedService(catalogTrackRepo, catalogTrackRepo, featuredBridge)
	listFeaturingSvc := catalogService.NewListFeaturingService(catalogTrackRepo)

	getTrackStatusSvc := catalogService.NewGetTrackStatusService(catalogTrackRepo)
	featuredArtistHandler := catalogHandler.NewFeaturedArtistHandler(backfillFeaturedSvc, listFeaturingSvc)
	trackHandler := catalogHandler.NewTrackHandler(addTrackSvc, listTracksSvc, getTrackStatusSvc, deleteTrackSvc, setTrackNumberSvc, featuredArtistHandler)
	playlistHandler := catalogHandler.NewPlaylistHandler(playlistLifecycleSvc, playlistMembershipSvc)
	streamTrackSvc := catalogService.NewStreamTrackService(catalogTrackRepo, audioStore, catalogService.WithStreamScheduler(scheduler), catalogService.WithStreamMetrics(audioStoreMetrics))
	streamHandler := catalogHandler.NewStreamHandler(streamTrackSvc)
	audioURLSvc := catalogService.NewAudioURLService(catalogTrackRepo, audioStore, catalogService.WithAudioURLMetrics(audioStoreMetrics))
	audioURLHandler := catalogHandler.NewAudioURLHandler(audioURLSvc)

	var retryH *acqHandler.RetryHandler
	var reacquireH *acqHandler.ReacquireHandler
	if scheduler != nil {
		retryH = acqHandler.NewRetryHandler(trackRepo, scheduler, acqService.NewRetryAdmission())
		reacquireH = acqHandler.NewReacquireHandler(trackRepo, a.scheduler, acqService.NewReacquireAdmission())
	}

	return catalogWiring{
		trackRepo:         trackRepo,
		setTrackNumberSvc: setTrackNumberSvc,
		trackHandler:      trackHandler,
		libraryHandler:    catalogHandler.NewLibraryHandler(catalogService.NewLibraryLensService(catalogTrackRepo)),
		playlistHandler:   playlistHandler,
		streamHandler:     streamHandler,
		audioURLHandler:   audioURLHandler,
		retryH:            retryH,
		reacquireH:        reacquireH,
	}, nil
}

func (a *App) buildStreamripSources() []acqPorts.AudioSource {
	var sources []acqPorts.AudioSource
	for _, service := range a.cfg.StreamripServices {
		service = strings.ToLower(strings.TrimSpace(service))
		if service == "" {
			continue
		}
		if !streamrip.Supported(service) {
			slog.Warn("acquisition: unsupported streamrip service ignored", "service", service)
			continue
		}
		sources = append(sources, streamrip.NewSource(service).WithBinary(a.cfg.StreamripBin))
		slog.Info("acquisition: streamrip source enabled", "service", service)
	}
	return sources
}

func (a *App) wirePlayback(trackRepo *persistence.PgxTrackRepository) *playbackHandler.QueueHandler {
	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool)
	nowPlayingReader := catalogbridge.NewNowPlayingReader(trackRepo)
	queueSvc := playbackService.NewQueueService(queueStateRepo, nowPlayingReader)
	return playbackHandler.NewQueueHandler(queueSvc)
}

func (a *App) wireFeedback() *feedbackHandler.FeedbackHandler {
	if !a.cfg.HasIssueTracker() {
		slog.Info("feedback: issue tracker not configured, in-app reports disabled")
		return nil
	}
	tracker := feedbackGithub.NewIssueTracker(a.cfg.GitHubIssueRepo, a.cfg.GitHubIssueToken)
	return feedbackHandler.NewFeedbackHandler(feedbackService.NewSubmitReportService(tracker))
}

func (a *App) mountRoutes(
	verifier auth.TokenVerifier,
	cat catalogWiring,
	queueHandler *playbackHandler.QueueHandler,
	discoveryH *discoveryHandler.DiscoveryHandler,
	feedbackH *feedbackHandler.FeedbackHandler,
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
		cat.streamHandler.Routes(r)
		cat.audioURLHandler.Routes(r)
		if cat.reacquireH != nil {
			r.Post("/tracks/{trackId}/reacquire", cat.reacquireH.HandleReacquire)
		}
		if cat.retryH != nil {
			r.Post("/tracks/{trackId}/retry", cat.retryH.HandleRetryAcquisition)
		}
		r.Mount("/library", cat.libraryHandler.Routes())
		r.Mount("/playlists", cat.playlistHandler.Routes())
		r.Mount("/playback", queueHandler.Routes())
		r.Mount("/discovery", discoveryH.Routes())
		if feedbackH != nil {
			r.Mount("/feedback", feedbackH.Routes())
		}
		r.Handle("/events", newSSEHandler(a.eventBus))
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
	a.whenLeader("eval meter", a.evalMeter.Start)
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

const stalePendingReconcileInterval = 10 * time.Minute

// startStalePendingReconcile sweeps tracks orphaned at pending by an acquisition
// job that died mid-flight, transitioning them to failed so the retry path can
// reclaim them. The ticker runs once on leader acquisition (startup recovery) and
// then on an interval (ongoing sweep).
func (a *App) startStalePendingReconcile(ctx context.Context, repo catalogPorts.StalePendingFailer) {
	svc := catalogService.NewReconcileStalePendingService(repo)
	a.startTicker(ctx, "stale pending reconcile", stalePendingReconcileInterval, func() {
		if _, err := svc.Execute(ctx); err != nil {
			slog.WarnContext(ctx, "stale pending reconcile failed", "error", err)
		}
	})
	slog.Info("stale pending reconcile started", "interval", stalePendingReconcileInterval.String())
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
		eventQuery := discoveryPersistence.NewPgxEventStore(a.pool)
		conditions = append(conditions, buildCoverageCondition(eventQuery, a.cfg.AlertZeroResultThreshold))
	}

	a.alertMonitor = adminAlert.NewMonitor(notifier, 30*time.Second, conditions...)
	a.whenLeader("alert monitor", a.alertMonitor.Start)
}

// coverageEvents is the slice of the discovery event query the coverage-gap
// alert needs: the unbounded true total for the threshold comparison, plus the
// top-N list for display context only.
type coverageEvents interface {
	ZeroResultTotal(ctx context.Context, since time.Time) (int, error)
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]discoveryPorts.QueryCount, error)
}

func buildCoverageCondition(eventQuery coverageEvents, threshold int) adminAlert.Condition {
	return adminAlert.Condition{
		Key: "coverage_zero_result",
		Eval: func(ctx context.Context) *adminAlert.Alert {
			since := time.Now().UTC().Add(-24 * time.Hour)
			// The threshold must compare against the true total: ZeroResultQueries
			// caps at the top 1000 distinct normalized queries, so summing it
			// silently undercounts once a window spans more than that many.
			total, err := eventQuery.ZeroResultTotal(ctx, since)
			if err != nil {
				slog.WarnContext(ctx, "coverage alert query failed", "error", err)
				return nil
			}
			if total < threshold {
				return nil
			}
			msg := fmt.Sprintf("zero-result searches in 24h: %d (threshold %d)", total, threshold)
			if rows, err := eventQuery.ZeroResultQueries(ctx, since, 1000); err == nil && len(rows) > 0 {
				msg += fmt.Sprintf("; top query %q (%d)", rows[0].QueryNorm, rows[0].Count)
			}
			return &adminAlert.Alert{
				Title:    "altune discovery coverage gap",
				Message:  msg,
				Severity: adminAlert.SeveritySignal,
			}
		},
	}
}

func (a *App) buildAudioStore() (catalogPorts.AudioStore, error) {
	if a.cfg.HasOCIS3() {
		store, err := storage.NewObjectStorageAudioStore(
			a.cfg.OCIS3Endpoint,
			a.cfg.OCIS3AccessKey,
			a.cfg.OCIS3SecretKey,
			a.cfg.OCIS3Bucket,
			a.cfg.OCIS3Region,
		)
		if err == nil {
			slog.Info("audio store: OCI Object Storage")
			return store, nil
		}
		if a.cfg.MusicDir == "" {
			return nil, fmt.Errorf("audio store: OCI S3 is configured but failed to initialize and no MUSIC_DIR fallback is set: %w", err)
		}
		slog.Warn("OCI S3 store failed to initialize, falling back to filesystem", "error", err)
	}

	if a.cfg.MusicDir != "" {
		slog.Info("audio store: filesystem", "dir", a.cfg.MusicDir)
		return storage.NewFilesystemAudioStore(a.cfg.MusicDir), nil
	}

	return nil, missingAudioStoreError(a.cfg)
}

// missingAudioStoreError names the configuration that would have wired a live
// audio store, so a misconfiguration fails at startup instead of panicking on
// the first stream or delete with a nil store.
func missingAudioStoreError(cfg *config.Config) error {
	var missing []string
	if cfg.OCIS3Endpoint == "" {
		missing = append(missing, "OCI_S3_ENDPOINT")
	}
	if cfg.OCIS3AccessKey == "" {
		missing = append(missing, "OCI_S3_ACCESS_KEY")
	}
	if cfg.OCIS3SecretKey == "" {
		missing = append(missing, "OCI_S3_SECRET_KEY")
	}
	if cfg.OCIS3Bucket == "" {
		missing = append(missing, "OCI_S3_BUCKET")
	}
	if len(missing) > 0 && len(missing) < 4 {
		return fmt.Errorf(
			"audio store: incomplete OCI S3 configuration, missing %s (set these for object storage, or set MUSIC_DIR for a filesystem store)",
			strings.Join(missing, ", "),
		)
	}
	return fmt.Errorf(
		"audio store: no backend configured; set MUSIC_DIR for a filesystem store, or all of OCI_S3_ENDPOINT, OCI_S3_ACCESS_KEY, OCI_S3_SECRET_KEY, OCI_S3_BUCKET for object storage",
	)
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
	a.startTicker(ctx, "behavioral corpus refresh", 24*time.Hour, func() {
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
	a.startTicker(ctx, "discovery metrics rollup", 6*time.Hour, func() {
		now := time.Now().UTC()
		for _, day := range []time.Time{now, now.Add(-24 * time.Hour)} {
			if err := store.RollupDay(ctx, day); err != nil {
				slog.WarnContext(ctx, "discovery metrics rollup failed", "error", err)
			}
		}
	})
	slog.Info("discovery metrics rollup started")
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
	a.whenLeader("vocabulary refresh", func(context.Context) {
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
