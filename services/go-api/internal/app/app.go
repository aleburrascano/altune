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
	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"
	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackService "altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil"
	sharedRedis "altune/go-api/internal/shared/redis"
	"altune/go-api/internal/shared/textnorm"

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

	var scheduler catalogPorts.AcquisitionScheduler
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
	queueSvc := playbackService.NewQueueService(queueStateRepo)
	queueHandler := playbackHandler.NewQueueHandler(queueSvc)

	var sharedMB *providers.MusicBrainzAdapter
	if a.cfg.HasMusicBrainz() {
		sharedMB = providers.NewMusicBrainzAdapter(
			newDiscoveryClient(),
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
			textnorm.NormalizeForMatch,
			discoveryCacheAdapters.WithMetaphone(discoveryService.MetaphoneKey),
		)
	}

	clickSvc := discoveryService.NewRecordClickService(clickRepo)
	historySvc := discoveryService.NewListSearchHistoryService(historyRepo)

	trackHandler := catalogHandler.NewTrackHandler(addTrackSvc, listTracksSvc, deleteTrackSvc)
	playlistHandler := catalogHandler.NewPlaylistHandler(playlistSvc)
	streamTrackSvc := catalogService.NewStreamTrackService(trackRepo, audioStore, scheduler)
	streamHandler := catalogHandler.NewStreamHandler(streamTrackSvc)
	deezerContentClient := newDiscoveryClient()
	deezerContent := providers.NewDeezerAdapter(deezerContentClient)
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
			buildArtworkChain(clientFactory{}, a.cfg),
			enrichmentCache,
			// Memoize each name resolution so the search path can attach the MBID to
			// a non-MB result later (cap 5 warm).
			discoveryService.WithMBIDMemo(enrichmentCache),
		)
	}

	discoveryH := discoveryHandler.NewDiscoveryHandler(discoveryHandler.DiscoveryServices{
		Search:  searchSvc,
		Click:   clickSvc,
		History: historySvc,
		Album:   albumSvc,
		Artist:  artistSvc,
		Related: relatedSvc,
		Enrich:  enrichSvc,
		Suggest: suggestSvc,
		Event:   eventSvc,
	})
	discoveryH.WithDetailEnrichers(a.buildDetailEnrichers())

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
		r.Handle("/events", &sseHandler{bus: a.eventBus})
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
