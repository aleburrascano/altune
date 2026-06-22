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
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/auth"
	authAdapters "altune/go-api/internal/auth/adapters"
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
	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackService "altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil"
	sharedRedis "altune/go-api/internal/shared/redis"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type App struct {
	cfg          *config.Config
	pool         *pgxpool.Pool
	redisClient  *goredis.Client
	server       *http.Server
	wg           sync.WaitGroup
	sem          chan struct{}
	scheduler    *acqService.BackgroundAcquisitionScheduler
	vocabRefresh *discoveryService.VocabularyRefreshService
	eventBus     *events.InProcessBus
}

func New(cfg *config.Config) *App {
	return &App{
		cfg: cfg,
		sem: make(chan struct{}, cfg.AcquisitionConcurrency),
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
	if a.vocabRefresh != nil {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer bgCancel()
		a.vocabRefresh.Shutdown(bgCtx)
	}
	if a.scheduler != nil {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer bgCancel()
		a.scheduler.Shutdown(bgCtx)
	} else {
		a.wg.Wait()
	}

	a.cleanup()
	slog.Info("shutdown complete")
	return nil
}

func (a *App) setup(ctx context.Context) error {
	var err error

	a.pool, err = database.NewPool(ctx, a.cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}

	a.redisClient = sharedRedis.NewClient(ctx, a.cfg.RedisURL)

	verifier, err := a.buildTokenVerifier(ctx)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	a.eventBus = events.NewInProcessBus()

	audioStore := a.buildAudioStore()
	trackRepo := persistence.NewPgxTrackRepository(a.pool)
	playlistRepo := persistence.NewPgxPlaylistRepository(a.pool)

	var audioSearcher acqPorts.AudioSearcher
	if audioStore != nil {
		audioSearcher = ytdlp.NewYtDlpAudioSearcher(a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime)
	}

	var scheduler acqService.AcquisitionScheduler
	if audioSearcher != nil && audioStore != nil {
		acquireSvc := acqService.NewAcquireTrackAudioService(trackRepo, audioSearcher, audioStore, acqService.WithAcquireEvents(a.eventBus))
		bgScheduler := acqService.NewBackgroundAcquisitionScheduler(acquireSvc, &a.wg, a.sem)
		a.scheduler = bgScheduler
		scheduler = bgScheduler
	}

	addTrackSvc := catalogService.NewAddTrackService(
		trackRepo,
		catalogService.WithAddTrackEvents(a.eventBus),
		catalogService.WithAcquisitionScheduler(scheduler),
	)
	listTracksSvc := catalogService.NewListTracksService(trackRepo)
	deleteTrackSvc := catalogService.NewDeleteTrackService(trackRepo, audioStore, catalogService.WithDeleteTrackEvents(a.eventBus))
	playlistSvc := catalogService.NewPlaylistService(playlistRepo, trackRepo, catalogService.WithPlaylistEvents(a.eventBus))

	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool)
	saveQueueStateSvc := playbackService.NewSaveQueueStateService(queueStateRepo)
	getQueueStateSvc := playbackService.NewGetQueueStateService(queueStateRepo)
	queueHandler := playbackHandler.NewQueueHandler(saveQueueStateSvc, getQueueStateSvc)

	var sharedMB *providers.MusicBrainzAdapter
	if a.cfg.HasMusicBrainz() {
		sharedMB = providers.NewMusicBrainzAdapter(
			&http.Client{Timeout: 10 * time.Second},
			a.cfg.MusicBrainzUserAgent,
		)
	}
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(a.pool)
	clickRepo := discoveryPersistence.NewPgxSearchClickRepository(a.pool)
	eventStore := discoveryPersistence.NewPgxEventStore(a.pool)

	// vocabStore is shared by suggest + the periodic vocabulary refresh; the
	// search pipeline builds its own inside BuildSearchService.
	var vocabStore discoveryPorts.VocabularyStore
	if a.redisClient != nil {
		vocabStore = discoveryCacheAdapters.NewVocabularyStore(
			a.redisClient,
			discoveryService.NormalizeForMatch,
			discoveryCacheAdapters.WithMetaphone(discoveryService.MetaphoneKey),
		)
	}

	clickSvc := discoveryService.NewRecordClickService(clickRepo)
	historySvc := discoveryService.NewListSearchHistoryService(historyRepo)

	trackHandler := catalogHandler.NewTrackHandler(addTrackSvc, listTracksSvc, deleteTrackSvc)
	playlistHandler := catalogHandler.NewPlaylistHandler(playlistSvc)
	streamTrackSvc := catalogService.NewStreamTrackService(trackRepo, audioStore, scheduler)
	streamHandler := catalogHandler.NewStreamHandler(streamTrackSvc)
	deezerContentClient := &http.Client{Timeout: 10 * time.Second}
	deezerContent := providers.NewDeezerAdapter(deezerContentClient)

	albumProviders := map[string]discoveryPorts.AlbumContentProvider{
		"deezer": deezerContent,
	}
	artistProviders := map[string]discoveryPorts.ArtistContentProvider{
		"deezer": deezerContent,
		// SoundCloud serves the underground long tail: an artist sourced from
		// SoundCloud carries its numeric user id, which keys these endpoints.
		"soundcloud": providers.NewSoundCloudAPIAdapter(&http.Client{Timeout: 10 * time.Second}, nil),
	}

	// Related tracks are track-keyed: a SoundCloud-sourced track carries its
	// numeric track id, which keys /tracks/{id}/related. SoundCloud-only today.
	relatedProviders := map[string]discoveryPorts.RelatedTracksProvider{
		"soundcloud": providers.NewSoundCloudAPIAdapter(&http.Client{Timeout: 10 * time.Second}, nil),
	}
	relatedSvc := discoveryService.NewGetRelatedTracksService(relatedProviders)

	albumSvc := discoveryService.NewGetAlbumTracksService(albumProviders)

	// Multi-provider consensus: ALL providers are equal sources, merged into a
	// union. Built via the shared BuildConsensusProviders so coverage signal B
	// measures the same provider set.
	consensusProviders := BuildConsensusProviders(a.cfg)

	var consensusOpts []discoveryService.ConsensusOption
	if sharedMB != nil {
		consensusOpts = append(consensusOpts, discoveryService.WithMBAuthority(sharedMB))
	}
	consensusSvc := discoveryService.NewConsensusService(consensusProviders, consensusOpts...)

	var artistContentOpts []discoveryService.ArtistContentOption
	artistContentOpts = append(artistContentOpts, discoveryService.WithConsensusService(consensusSvc))
	artistSvc := discoveryService.NewGetArtistContentService(artistProviders, artistContentOpts...)
	suggestSvc := discoveryService.NewSuggestService(vocabStore)

	searchSvc := BuildSearchService(a.cfg, a.pool, a.redisClient, eventStore)

	eventSvc := discoveryService.NewRecordEventService(eventStore)

	// MusicBrainz detail-open enrichment: genres/year/rating/external-ids + the
	// HD MBID-keyed cover via the existing artwork chain. Only when MB is
	// configured; nil otherwise (the handler degrades to an empty DTO).
	var enrichSvc *discoveryService.EnrichmentService
	if sharedMB != nil {
		enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(a.redisClient)
		enrichSvc = discoveryService.NewEnrichmentService(
			sharedMB,
			buildArtworkChain(a.cfg),
			enrichmentCache,
			// Memoize each name resolution so the search path can attach the MBID to
			// a non-MB result later (cap 5 warm).
			discoveryService.WithMBIDMemo(enrichmentCache),
		)
	}

	discoveryH := discoveryHandler.NewDiscoveryHandler(searchSvc, clickSvc, historySvc, albumSvc, artistSvc, relatedSvc, enrichSvc, suggestSvc, eventSvc)

	a.startVocabularyRefresh(vocabStore)

	var retryH *acqHandler.RetryHandler
	if scheduler != nil {
		retryH = acqHandler.NewRetryHandler(trackRepo, scheduler)
	}

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

		r.Mount("/tracks", trackHandler.Routes())
		r.Get("/tracks/{trackId}/audio", streamHandler.HandleStreamAudio)
		if retryH != nil {
			r.Post("/tracks/{trackId}/retry", retryH.HandleRetryAcquisition)
		}
		r.Mount("/playlists", playlistHandler.Routes())
		r.Mount("/playback", queueHandler.Routes())
		r.Mount("/discovery", discoveryH.Routes())
		r.Handle("/events", events.NewSSEHandler(a.eventBus))
	})

	a.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return nil
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := database.CheckHealth(r.Context(), a.pool)

	dbStatus := "ok"
	if !status.OK {
		dbStatus = "down"
	}
	if a.pool == nil {
		dbStatus = "not_configured"
	}

	redisStatus := "ok"
	if a.redisClient == nil {
		redisStatus = "not_configured"
	} else if err := a.redisClient.Ping(r.Context()).Err(); err != nil {
		redisStatus = "down"
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"db":"%s","redis":"%s"}`, dbStatus, redisStatus)
}

func (a *App) buildTokenVerifier(ctx context.Context) (auth.TokenVerifier, error) {
	return authAdapters.NewSupabaseJWTVerifier(
		ctx,
		a.cfg.SupabaseJWTJWKSURL,
		a.cfg.SupabaseProjectURL,
		a.cfg.SupabaseJWTAud,
	)
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

func buildDiscoveryProviders(cfg *config.Config, mb *providers.MusicBrainzAdapter) []discoveryPorts.SearchProvider {
	var providerList []discoveryPorts.SearchProvider

	deezerClient := &http.Client{Timeout: 10 * time.Second}
	providerList = append(providerList, providers.NewDeezerAdapter(deezerClient))

	itunesClient := &http.Client{Timeout: 10 * time.Second}
	providerList = append(providerList, providers.NewITunesAdapter(itunesClient))

	providerList = append(providerList, providers.NewTheAudioDBAdapter(&http.Client{Timeout: 10 * time.Second}))

	if mb != nil {
		providerList = append(providerList, mb)
	}

	if cfg.HasLastFM() {
		lfmClient := &http.Client{Timeout: 10 * time.Second}
		providerList = append(providerList, providers.NewLastFmAdapter(lfmClient, cfg.LastFMAPIKey))
	}

	// Direct api-v2 SoundCloud client (coverage: unreleased/underground long tail),
	// with the yt-dlp adapter as fallback when client_id resolution is down.
	soundcloudClient := &http.Client{Timeout: 10 * time.Second}
	providerList = append(providerList, providers.NewSoundCloudAPIAdapter(
		soundcloudClient,
		providers.NewSoundCloudAdapter(),
	))

	providerList = append(providerList, providers.NewYouTubeMusicAdapter())

	slog.Info("discovery providers configured", "count", len(providerList))
	return providerList
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
	deezerClient := &http.Client{Timeout: 15 * time.Second}
	charts = append(charts, providers.NewDeezerAdapter(deezerClient))
	if a.cfg.HasLastFM() {
		lfmClient := &http.Client{Timeout: 15 * time.Second}
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
