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
	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackService "altune/go-api/internal/playback/service"
	"altune/go-api/internal/discovery/adapters/providers"
	domain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
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

	var audioSearcher catalogPorts.AudioSearcher
	if audioStore != nil {
		audioSearcher = ytdlp.NewYtDlpAudioSearcher(a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime)
	}

	addTrackSvc := catalogService.NewAddTrackService(trackRepo, catalogService.WithAddTrackEvents(a.eventBus))
	listTracksSvc := catalogService.NewListTracksService(trackRepo)
	deleteTrackSvc := catalogService.NewDeleteTrackService(trackRepo, audioStore, catalogService.WithDeleteTrackEvents(a.eventBus))
	reconcileSvc := catalogService.NewReconcileTrackStatusService(trackRepo, audioStore)
	playlistSvc := catalogService.NewPlaylistService(playlistRepo, trackRepo, catalogService.WithPlaylistEvents(a.eventBus))

	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool)
	saveQueueStateSvc := playbackService.NewSaveQueueStateService(queueStateRepo)
	getQueueStateSvc := playbackService.NewGetQueueStateService(queueStateRepo)
	queueHandler := playbackHandler.NewQueueHandler(saveQueueStateSvc, getQueueStateSvc)

	var scheduler acqService.AcquisitionScheduler
	if audioSearcher != nil && audioStore != nil {
		acquireSvc := acqService.NewAcquireTrackAudioService(trackRepo, audioSearcher, audioStore, acqService.WithAcquireEvents(a.eventBus))
		bgScheduler := acqService.NewBackgroundAcquisitionScheduler(acquireSvc, &a.wg, a.sem)
		a.scheduler = bgScheduler
		scheduler = bgScheduler
	}

	var sharedMB *providers.MusicBrainzAdapter
	if a.cfg.HasMusicBrainz() {
		sharedMB = providers.NewMusicBrainzAdapter(
			&http.Client{Timeout: 10 * time.Second},
			a.cfg.MusicBrainzUserAgent,
		)
	}
	searchProviders := a.buildDiscoveryProviders(sharedMB)
	queryCache := discoveryCacheAdapters.NewRedisQueryCache(a.redisClient)
	circuitBreaker := discoveryService.NewCircuitBreaker()
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(a.pool)
	clickRepo := discoveryPersistence.NewPgxSearchClickRepository(a.pool)
	// AIDEV-DECISION: artwork chain — ID-based sources first, name-search last.
	// ID-based (always correct for the entity): Cover Art Archive → Fanart.tv
	// Name-search fallback (risk of wrong artist): Genius → TheAudioDB → Deezer → iTunes → YouTube
	var artworkResolvers []discoveryPorts.ArtworkResolver
	artworkResolvers = append(artworkResolvers,
		providers.NewCoverArtArchiveResolver(&http.Client{Timeout: 10 * time.Second}))
	if a.cfg.HasFanartTV() {
		artworkResolvers = append(artworkResolvers,
			providers.NewFanartTvArtworkResolver(&http.Client{Timeout: 10 * time.Second}, a.cfg.FanartTVAPIKey))
	}
	var sharedDiscogs *providers.DiscogsAdapter
	if a.cfg.HasDiscogs() {
		sharedDiscogs = providers.NewDiscogsAdapter(
			&http.Client{Timeout: 10 * time.Second},
			a.cfg.DiscogsToken,
			a.cfg.MusicBrainzUserAgent,
		)
	}
	if a.cfg.HasGenius() {
		artworkResolvers = append(artworkResolvers,
			providers.NewGeniusArtworkResolver(&http.Client{Timeout: 10 * time.Second}, a.cfg.GeniusAccessToken))
	}
	artworkResolvers = append(artworkResolvers,
		providers.NewTheAudioDBAdapter(&http.Client{Timeout: 10 * time.Second}),
		providers.NewDeezerAdapter(&http.Client{Timeout: 10 * time.Second}),
		providers.NewITunesAdapter(&http.Client{Timeout: 10 * time.Second}),
	)
	if a.cfg.HasYouTube() {
		artworkResolvers = append(artworkResolvers,
			providers.NewYouTubeArtworkResolver(&http.Client{Timeout: 10 * time.Second}, a.cfg.YouTubeAPIKey))
	}
	artworkChain := providers.NewChainedArtworkResolver(artworkResolvers...)

	searchOpts := []discoveryService.SearchOption{
		discoveryService.WithArtworkResolver(artworkChain),
	}
	if a.redisClient != nil {
		searchOpts = append(searchOpts, discoveryService.WithArtworkCache(
			discoveryCacheAdapters.NewRedisArtworkCache(a.redisClient),
		))
	}

	var vocabStore discoveryPorts.VocabularyStore
	if a.redisClient != nil {
		vocabStore = discoveryCacheAdapters.NewVocabularyStore(
			a.redisClient,
			discoveryService.NormalizeForMatch,
			discoveryCacheAdapters.WithMetaphone(discoveryService.MetaphoneKey),
		)
	}
	if vocabStore != nil {
		searchOpts = append(searchOpts, discoveryService.WithVocabularyStore(vocabStore))
	}

	searchOpts = append(searchOpts, discoveryService.WithClickSignals(clickRepo))
	clickSvc := discoveryService.NewRecordClickService(clickRepo)
	historySvc := discoveryService.NewListSearchHistoryService(historyRepo)

	trackHandler := catalogHandler.NewTrackHandler(addTrackSvc, listTracksSvc, deleteTrackSvc, reconcileSvc, scheduler)
	playlistHandler := catalogHandler.NewPlaylistHandler(playlistSvc)
	streamTrackSvc := catalogService.NewStreamTrackService(trackRepo, audioStore, reconcileSvc, scheduler)
	streamHandler := catalogHandler.NewStreamHandler(streamTrackSvc)
	deezerContentClient := &http.Client{Timeout: 10 * time.Second}
	deezerContent := providers.NewDeezerAdapter(deezerContentClient)
	ytmusicContent := providers.NewYouTubeMusicAdapter()

	albumProviders := map[string]discoveryPorts.AlbumContentProvider{
		"deezer": deezerContent,
	}
	artistProviders := map[string]discoveryPorts.ArtistContentProvider{
		"deezer": deezerContent,
	}

	var tidalContent *providers.TidalAdapter
	if a.cfg.HasTidal() {
		tidalContent = providers.NewTidalAdapter(
			&http.Client{Timeout: 15 * time.Second},
			a.cfg.TidalClientID,
			a.cfg.TidalClientSecret,
		)
		artistProviders["tidal"] = tidalContent
	}

	albumSvc := discoveryService.NewGetAlbumTracksService(albumProviders)

	if sharedMB != nil {
		searchOpts = append(searchOpts, discoveryService.WithIdentityResolver(sharedMB))
	}

	// Multi-provider consensus: ALL providers are equal sources.
	// Albums are merged from every provider into a union — no single
	// provider is "primary." No hardcoded timeout — uses request context.
	var consensusProviders []discoveryService.ConsensusProvider
	if a.cfg.HasLastFM() {
		lfm := providers.NewLastFmAdapter(&http.Client{Timeout: 10 * time.Second}, a.cfg.LastFMAPIKey)
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "lastfm",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				return lfm.GetArtistAlbums(ctx, domain.ProviderLastFM, artistName)
			},
		})
	}
	if sharedMB != nil {
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "musicbrainz",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				validated, err := sharedMB.ValidateArtistAlbums(ctx, artistName, nil)
				if err != nil || validated == nil {
					return nil, err
				}
				return validated.Confirmed, nil
			},
		})
	}
	if sharedDiscogs != nil {
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "discogs",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				info, err := sharedDiscogs.ResolveDiscogsArtist(ctx, artistName, nil)
				if err != nil || info == nil {
					return nil, err
				}
				releases, err := sharedDiscogs.FetchArtistReleases(ctx, info.ID)
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
	itunesConsensus := providers.NewITunesAdapter(&http.Client{Timeout: 10 * time.Second})
	consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
		Name: "itunes",
		Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
			return itunesConsensus.Search(ctx, artistName, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
		},
	})

	if tidalContent != nil {
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "tidal",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				return tidalContent.GetArtistAlbums(ctx, domain.ProviderTidal, artistName)
			},
		})
	}

	consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
		Name: "ytmusic",
		Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
			return ytmusicContent.GetArtistAlbums(ctx, domain.ProviderYouTube, artistName)
		},
	})

	var consensusOpts []discoveryService.ConsensusOption
	if sharedMB != nil {
		consensusOpts = append(consensusOpts, discoveryService.WithConsensusMB(sharedMB))
	}
	consensusSvc := discoveryService.NewConsensusService(consensusProviders, consensusOpts...)

	var artistContentOpts []discoveryService.ArtistContentOption
	artistContentOpts = append(artistContentOpts, discoveryService.WithConsensusService(consensusSvc))
	artistSvc := discoveryService.NewGetArtistContentService(artistProviders, artistContentOpts...)
	suggestSvc := discoveryService.NewSuggestService(vocabStore)

	findRelatedSvc := discoveryService.NewFindRelatedService(trackRepo, deezerContent, deezerContent)
	searchOpts = append(searchOpts, discoveryService.WithFindRelatedService(findRelatedSvc))

	searchSvc := discoveryService.NewSearchMusicService(searchProviders, queryCache, historyRepo, circuitBreaker, searchOpts...)

	discoveryH := discoveryHandler.NewDiscoveryHandler(searchSvc, clickSvc, historySvc, albumSvc, artistSvc, suggestSvc)

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

func (a *App) buildDiscoveryProviders(mb *providers.MusicBrainzAdapter) []discoveryPorts.SearchProvider {
	var providerList []discoveryPorts.SearchProvider

	deezerClient := &http.Client{Timeout: 10 * time.Second}
	providerList = append(providerList, providers.NewDeezerAdapter(deezerClient))

	itunesClient := &http.Client{Timeout: 10 * time.Second}
	providerList = append(providerList, providers.NewITunesAdapter(itunesClient))

	providerList = append(providerList, providers.NewTheAudioDBAdapter(&http.Client{Timeout: 10 * time.Second}))

	if mb != nil {
		providerList = append(providerList, mb)
	}

	if a.cfg.HasLastFM() {
		lfmClient := &http.Client{Timeout: 10 * time.Second}
		providerList = append(providerList, providers.NewLastFmAdapter(lfmClient, a.cfg.LastFMAPIKey))
	}

	providerList = append(providerList, providers.NewSoundCloudAdapter())

	if a.cfg.HasTidal() {
		tidalClient := &http.Client{Timeout: 10 * time.Second}
		providerList = append(providerList, providers.NewTidalAdapter(tidalClient, a.cfg.TidalClientID, a.cfg.TidalClientSecret))
	}

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
