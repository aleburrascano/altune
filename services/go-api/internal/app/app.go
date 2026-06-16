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

	"altune/go-api/internal/auth"
	authAdapters "altune/go-api/internal/auth/adapters"
	acqHandler "altune/go-api/internal/acquisition/adapters/handler"
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
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
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/httputil"
	sharedRedis "altune/go-api/internal/shared/redis"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type App struct {
	cfg         *config.Config
	pool        *pgxpool.Pool
	redisClient *goredis.Client
	server      *http.Server
	wg          sync.WaitGroup
	sem         chan struct{}
	scheduler   *acqService.BackgroundAcquisitionScheduler
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

	audioStore := a.buildAudioStore()
	trackRepo := persistence.NewPgxTrackRepository(a.pool)
	playlistRepo := persistence.NewPgxPlaylistRepository(a.pool)

	var audioSearcher catalogPorts.AudioSearcher
	if audioStore != nil {
		audioSearcher = ytdlp.NewYtDlpAudioSearcher(a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime)
	}

	addTrackSvc := catalogService.NewAddTrackService(trackRepo)
	listTracksSvc := catalogService.NewListTracksService(trackRepo)
	deleteTrackSvc := catalogService.NewDeleteTrackService(trackRepo, audioStore)
	reconcileSvc := catalogService.NewReconcileTrackStatusService(trackRepo, audioStore)
	playlistSvc := catalogService.NewPlaylistService(playlistRepo, trackRepo)

	var scheduler acqService.AcquisitionScheduler
	if audioSearcher != nil && audioStore != nil {
		acquireSvc := acqService.NewAcquireTrackAudioService(trackRepo, audioSearcher, audioStore)
		bgScheduler := acqService.NewBackgroundAcquisitionScheduler(acquireSvc, &a.wg, a.sem)
		a.scheduler = bgScheduler
		scheduler = bgScheduler
	}

	searchProviders := a.buildDiscoveryProviders()
	queryCache := discoveryCacheAdapters.NewRedisQueryCache(a.redisClient)
	circuitBreaker := discoveryService.NewCircuitBreaker()
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(a.pool)
	clickRepo := discoveryPersistence.NewPgxSearchClickRepository(a.pool)
	artworkChain := providers.NewChainedArtworkResolver(
		providers.NewDeezerAdapter(&http.Client{Timeout: 10 * time.Second}),
		providers.NewTheAudioDBAdapter(&http.Client{Timeout: 10 * time.Second}),
	)

	searchOpts := []discoveryService.SearchOption{
		discoveryService.WithArtworkResolver(artworkChain),
	}
	if a.cfg.HasFanartTV() {
		searchOpts = append(searchOpts, discoveryService.WithFanartResolver(
			providers.NewFanartTvArtworkResolver(&http.Client{Timeout: 10 * time.Second}, a.cfg.FanartTVAPIKey),
		))
	}
	if a.cfg.HasGenius() {
		searchOpts = append(searchOpts, discoveryService.WithGeniusResolver(
			providers.NewGeniusArtworkResolver(&http.Client{Timeout: 10 * time.Second}, a.cfg.GeniusAccessToken),
		))
	}
	if a.redisClient != nil {
		searchOpts = append(searchOpts, discoveryService.WithArtworkCache(
			discoveryCacheAdapters.NewRedisArtworkCache(a.redisClient),
		))
	}
	searchSvc := discoveryService.NewSearchMusicService(searchProviders, queryCache, historyRepo, circuitBreaker, searchOpts...)

	clickSvc := discoveryService.NewRecordClickService(clickRepo)
	historySvc := discoveryService.NewListSearchHistoryService(historyRepo)

	trackHandler := catalogHandler.NewTrackHandler(addTrackSvc, listTracksSvc, deleteTrackSvc, reconcileSvc, scheduler)
	playlistHandler := catalogHandler.NewPlaylistHandler(playlistSvc)
	streamTrackSvc := catalogService.NewStreamTrackService(trackRepo, audioStore, reconcileSvc, scheduler)
	streamHandler := catalogHandler.NewStreamHandler(streamTrackSvc)
	deezerContentClient := &http.Client{Timeout: 10 * time.Second}
	deezerContent := providers.NewDeezerAdapter(deezerContentClient)

	albumProviders := map[string]discoveryPorts.AlbumContentProvider{
		"deezer": deezerContent,
	}
	artistProviders := map[string]discoveryPorts.ArtistContentProvider{
		"deezer": deezerContent,
	}
	albumSvc := discoveryService.NewGetAlbumTracksService(albumProviders)
	artistSvc := discoveryService.NewGetArtistContentService(artistProviders)

	discoveryH := discoveryHandler.NewDiscoveryHandler(searchSvc, clickSvc, historySvc, albumSvc, artistSvc)

	var retryH *acqHandler.RetryHandler
	if scheduler != nil {
		retryH = acqHandler.NewRetryHandler(trackRepo, scheduler)
	}

	r := chi.NewRouter()

	r.Use(httputil.CorrelationID)
	r.Use(httputil.Recoverer)
	r.Use(httputil.RequestLogger)
	r.Use(httputil.MaxBodySize(1 << 20)) // 1MB limit on request bodies
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   a.cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "ngrok-skip-browser-warning"},
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
		r.Mount("/discovery", discoveryH.Routes())
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

func (a *App) buildDiscoveryProviders() []discoveryPorts.SearchProvider {
	var providerList []discoveryPorts.SearchProvider

	deezerClient := &http.Client{Timeout: 10 * time.Second}
	providerList = append(providerList, providers.NewDeezerAdapter(deezerClient))

	itunesClient := &http.Client{Timeout: 10 * time.Second}
	providerList = append(providerList, providers.NewITunesAdapter(itunesClient))

	providerList = append(providerList, providers.NewTheAudioDBAdapter(&http.Client{Timeout: 10 * time.Second}))

	if a.cfg.HasMusicBrainz() {
		mbClient := &http.Client{Timeout: 10 * time.Second}
		providerList = append(providerList, providers.NewMusicBrainzAdapter(mbClient, a.cfg.MusicBrainzUserAgent))
	}

	if a.cfg.HasLastFM() {
		lfmClient := &http.Client{Timeout: 10 * time.Second}
		providerList = append(providerList, providers.NewLastFmAdapter(lfmClient, a.cfg.LastFMAPIKey))
	}

	providerList = append(providerList, providers.NewSoundCloudAdapter())

	slog.Info("discovery providers configured", "count", len(providerList))
	return providerList
}

func (a *App) cleanup() {
	if a.pool != nil {
		a.pool.Close()
	}
	if a.redisClient != nil {
		a.redisClient.Close()
	}
}
