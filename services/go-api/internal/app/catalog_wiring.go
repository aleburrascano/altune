package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/ytminnertube"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

type catalogWiring struct {
	trackRepo         *catalog.PgxTrackRepository
	audioStore        catalog.AudioStore
	orphanedAudio     *catalog.PgxOrphanedAudioRepository
	setTrackNumberSvc *catalog.SetTrackNumberService
	trackHandler      *catalog.TrackHandler
	libraryHandler    *catalog.LibraryHandler
	playlistHandler   *catalog.PlaylistHandler
	streamHandler     *catalog.StreamHandler
	audioURLHandler   *catalog.AudioURLHandler
	retryH            *acquisition.RetryHandler
	reacquireH        *acquisition.ReacquireHandler
	ytDlpSearcher     *acquisition.YtDlpAudioSearcher
	ytDlpAvailable    bool
	ytmCanary         *acquisition.YtmSearchClient
	scheduler         *acquisition.BackgroundAcquisitionScheduler
}

type audioSourcesStaging struct {
	audioStore     catalog.AudioStore
	trackRepo      *catalog.PgxTrackRepository
	ledger         *acquisition.TrackLedger
	scheduler      catalog.AcquisitionScheduler
	background     *acquisition.BackgroundAcquisitionScheduler
	ytDlpSearcher  *acquisition.YtDlpAudioSearcher
	ytDlpAvailable bool
	ytmCanary      *acquisition.YtmSearchClient
}

type catalogServicesStaging struct {
	catalogTrackRepo      *catalog.PgxCatalogTrackRepository
	orphanedAudio         *catalog.PgxOrphanedAudioRepository
	addTrackSvc           *catalog.AddTrackService
	listTracksSvc         *catalog.ListTracksService
	deleteTrackSvc        *catalog.DeleteTrackService
	setTrackNumberSvc     *catalog.SetTrackNumberService
	playlistLifecycleSvc  *catalog.PlaylistLifecycleService
	playlistMembershipSvc *catalog.PlaylistMembershipService
	backfillFeaturedSvc   *catalog.BackfillFeaturedService
	listFeaturingSvc      *catalog.ListFeaturingService
	getTrackStatusSvc     *catalog.GetTrackStatusService
	streamTrackSvc        *catalog.StreamTrackService
	audioURLSvc           *catalog.AudioURLService
}

func (a *App) wireCatalog(
	ctx context.Context,
	tap *observe.EventTap,
	disc *discovery.Discovery,
) (catalogWiring, error) {
	acquisition.SweepStaleTempDirs(acquisition.NewTempWorkspace())

	audio, err := a.wireAudioSources(ctx, tap, disc)
	if err != nil {
		return catalogWiring{}, err
	}
	services := a.wireCatalogServices(tap, featuredBridge(disc), audio)
	return a.wireCatalogHandlers(audio, services), nil
}

func featuredBridge(disc *discovery.Discovery) *catalog.FeaturedResolver {
	if disc.FeaturedArtists == nil {
		return nil
	}
	return catalog.NewFeaturedResolver(disc.FeaturedArtists)
}

func (a *App) wireAudioSources(
	ctx context.Context,
	tap *observe.EventTap,
	disc *discovery.Discovery,
) (audioSourcesStaging, error) {
	audioStore, err := catalog.FromConfig(a.cfg, slog.Default())
	if err != nil {
		return audioSourcesStaging{}, err
	}
	trackRepo := catalog.NewPgxTrackRepository(a.pool)

	var audioSources sourceTiers
	var tools acquisition.AcquisitionVerification
	if audioStore != nil {
		audioSources, tools = a.acquisitionSources(ctx)
	}

	staging := audioSourcesStaging{
		audioStore:     audioStore,
		trackRepo:      trackRepo,
		ledger:         acquisition.NewTrackLedger(trackRepo),
		ytDlpSearcher:  a.sourceCanarySearcher(),
		ytDlpAvailable: tools.YtDlp,
		ytmCanary:      a.sourceCanaryYTMClient(),
	}
	if len(audioSources.all()) > 0 && audioStore != nil {
		bgScheduler := a.buildAcquisitionScheduler(ctx, tap, disc, staging.ledger, audioStore, audioSources, tools)
		staging.background = bgScheduler
		staging.scheduler = bgScheduler
	}
	return staging, nil
}

func (a *App) buildAcquisitionScheduler(
	ctx context.Context,
	tap *observe.EventTap,
	disc *discovery.Discovery,
	ledger *acquisition.TrackLedger,
	audioStore catalog.AudioStore,
	audioSources sourceTiers,
	verification acquisition.AcquisitionVerification,
) *acquisition.BackgroundAcquisitionScheduler {
	skips := &acquisition.VerifySkipCounter{}
	audioProber := acquisition.NewFfprobeProber(a.cfg.FFmpegLocation, acquisition.WithSkipRecorder(skips))
	verification.Ffprobe, verification.Ffmpeg = audioProber.Available()

	acquireOpts := []func(*acquisition.AcquireTrackAudioService){
		acquisition.WithAcquireEvents(tap),
		acquisition.WithAcquireOrphanQueue(acquisition.NewOrphanedAudioRecorder(catalog.NewPgxOrphanedAudioRepository(a.pool))),
		acquisition.WithSupersededAudioGrace(a.cfg.AcquisitionSupersededAudioGrace),
		acquisition.WithAudioProber(audioProber),
		acquisition.WithVerifySkips(skips),
		acquisition.WithAudioTagger(acquisition.NewFFmpegTagger(binpath.Resolve("ffmpeg", a.cfg.FFmpegLocation), acquisition.NewId3Tagger())),
		acquisition.WithAcquireStoreKeyPrefix(a.cfg.AudioKeyPrefix),
		acquisition.WithRejectionStore(acquisition.NewPgxRejectionStore(a.pool)),
	}
	if disc.Search != nil {
		var resolverOpts []func(*acquisition.RecordingResolver)
		if disc.Recordings != nil {
			resolverOpts = append(resolverOpts,
				acquisition.WithISRCAuthority(disc.Recordings),
				acquisition.WithRerecordingAuthority(disc.Recordings),
				acquisition.WithRecordingLookup(acquisition.NewRecordingLookup(disc.Recordings)))
		}
		recordingResolver := acquisition.NewRecordingResolver(disc.Search, resolverOpts...)
		acquireOpts = append(acquireOpts, acquisition.WithRecordingResolver(recordingResolver))
		if disc.Recordings != nil {
			acquireOpts = append(acquireOpts,
				acquisition.WithAcquireRecordingLookup(recordingResolver),
				acquisition.WithRerecordingFinder(recordingResolver))
		}
	}
	lookups := &lookupTracker{}
	if a.cfg.AcoustIDAPIKey != "" {
		identifier := acquisition.NewChromaprintIdentifier(a.cfg.FFmpegLocation, a.cfg.AcoustIDAPIKey)
		verification.Fpcalc = identifier.Available()
		lookups.AudioIdentifier = identifier
		acquireOpts = append(acquireOpts, acquisition.WithAudioIdentifier(lookups))
		slog.InfoContext(ctx, "acquisition: fingerprint verification wired", "fpcalc", verification.Fpcalc)
	} else {
		slog.WarnContext(ctx, "acquisition: fingerprint verification disabled", "reason", "ACOUSTID_API_KEY is empty")
	}
	acquireOpts = append(acquireOpts,
		acquisition.WithDownloadLimiter(acquisition.NewDownloadLimiter(a.cfg.AcquisitionDownloadConcurrency)),
		acquisition.WithAcquireConfidenceFloor(a.cfg.AcquisitionConfidenceFloor),
		acquisition.WithAcquireTempWorkspace(acquisition.NewTempWorkspace()),
		acquisition.WithStoredAudioReader(storedAudioReader{store: audioStore}))
	if a.upgradeEnabled() {
		acquireOpts = append(acquireOpts, acquisition.WithPreviewMatcher(a.upgradePreviewMatcher(), a.cfg.AcquisitionUpgradePreviewAMaxBER))
	}
	acquireSvc := acquisition.NewAcquireTrackAudioService(
		ledger,
		acquisition.NewSourceRegistry(audioSources.first...).WithFallback(audioSources.fallback...),
		audioStore,
		acquireOpts...,
	)
	schedulerOpts := []func(*acquisition.BackgroundAcquisitionScheduler){
		acquisition.WithSchedulerPaused(a.cfg.AcquisitionPaused),
		acquisition.WithSchedulerEvents(tap),
		acquisition.WithVerificationStatus(verification),
		acquisition.WithVerifySkipCount(skips.Count),
		acquisition.WithFingerprintVerified(lookups.Verified),
		acquisition.WithDrainBudget(time.Duration(a.cfg.AcquisitionDrainBudgetSeconds) * time.Second),
	}
	if a.upgradeEnabled() {
		schedulerOpts = append(schedulerOpts, a.upgradeLane(ledger, acquireSvc))
	}
	if a.pool != nil {
		schedulerOpts = append(schedulerOpts,
			acquisition.WithJobQueue(acquisition.NewPgxJobQueue(a.pool)),
			acquisition.WithJobNotifier(acquisition.NewPgxJobNotifier(a.pool)),
			acquisition.WithOutcomeRecorder(acquisition.NewPgxOutcomeStore(a.pool)))
	}
	return acquisition.NewBackgroundAcquisitionScheduler(acquireSvc, &a.wg, a.sem, schedulerOpts...)
}

type storedAudioReader struct {
	store catalog.AudioStore
}

func (r storedAudioReader) CopyStored(ctx context.Context, audioRef, dir string) (string, error) {
	stream, _, err := r.store.Stream(ctx, audioRef)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()

	path := filepath.Join(dir, "stored"+filepath.Ext(audioRef))
	dst, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dst, stream); err != nil {
		_ = dst.Close()
		return "", err
	}
	return path, dst.Close()
}

type lookupTracker struct {
	acquisition.AudioIdentifier
	succeeded atomic.Bool
}

func (t *lookupTracker) Identify(ctx context.Context, filePath string, fullLengthForPreview float64) (acquisition.RecordingMatch, error) {
	match, err := t.AudioIdentifier.Identify(ctx, filePath, fullLengthForPreview)
	switch {
	case err == nil:
		t.succeeded.Store(true)
	case !errors.Is(err, acquisition.ErrIdentifyThrottled):
		t.succeeded.Store(false)
	}
	return match, err
}

func (t *lookupTracker) Verified() bool { return t.succeeded.Load() }

func (a *App) wireCatalogServices(
	tap *observe.EventTap,
	featuredBridge *catalog.FeaturedResolver,
	audio audioSourcesStaging,
) catalogServicesStaging {
	catalogTrackRepo := catalog.NewPgxCatalogTrackRepository(a.pool)
	playlistRepo := catalog.NewPgxPlaylistRepository(a.pool)
	orphanedAudio := catalog.NewPgxOrphanedAudioRepository(a.pool)
	audioStoreMetrics := catalog.NewExpvarAudioStoreMetrics()
	catalog.SetDBCallMetrics(catalog.NewExpvarDBCallMetrics())

	return catalogServicesStaging{
		catalogTrackRepo: catalogTrackRepo,
		orphanedAudio:    orphanedAudio,
		addTrackSvc: catalog.NewAddTrackService(
			catalogTrackRepo,
			catalog.WithAddTrackEvents(tap),
			catalog.WithAcquisitionScheduler(audio.scheduler),
			catalog.WithAddTrackClock(time.Now),
		),
		listTracksSvc:         catalog.NewListTracksService(catalogTrackRepo),
		deleteTrackSvc:        catalog.NewDeleteTrackService(catalogTrackRepo, audio.audioStore, catalog.WithDeleteTrackEvents(tap), catalog.WithDeleteTrackMetrics(audioStoreMetrics), catalog.WithDeleteTrackOrphanQueue(orphanedAudio)),
		setTrackNumberSvc:     catalog.NewSetTrackNumberService(catalogTrackRepo),
		playlistLifecycleSvc:  catalog.NewPlaylistLifecycleService(playlistRepo, catalog.WithPlaylistLifecycleEvents(tap)),
		playlistMembershipSvc: catalog.NewPlaylistMembershipService(playlistRepo, catalogTrackRepo, catalog.WithPlaylistMembershipEvents(tap)),
		backfillFeaturedSvc:   catalog.NewBackfillFeaturedService(catalogTrackRepo, catalogTrackRepo, featuredBridge),
		listFeaturingSvc:      catalog.NewListFeaturingService(catalogTrackRepo),
		getTrackStatusSvc:     catalog.NewGetTrackStatusService(catalogTrackRepo),
		streamTrackSvc:        catalog.NewStreamTrackService(catalogTrackRepo, audio.audioStore, catalog.WithStreamScheduler(audio.scheduler), catalog.WithStreamMetrics(audioStoreMetrics), catalog.WithStreamRecoverySwitch(a.jobSwitch(jobStreamRecovery))),
		audioURLSvc:           catalog.NewAudioURLService(catalogTrackRepo, audio.audioStore, catalog.WithAudioURLMetrics(audioStoreMetrics)),
	}
}

func (a *App) wireCatalogHandlers(audio audioSourcesStaging, svc catalogServicesStaging) catalogWiring {
	featuredArtistHandler := catalog.NewFeaturedArtistHandler(svc.backfillFeaturedSvc, svc.listFeaturingSvc)

	var retryH *acquisition.RetryHandler
	var reacquireH *acquisition.ReacquireHandler
	if audio.scheduler != nil {
		cooldowns := acquisition.NewPgxCooldownStore(a.pool)
		retryH = acquisition.NewRetryHandler(audio.ledger, audio.scheduler, acquisition.NewRetryAdmission(cooldowns))
		reacquireH = acquisition.NewReacquireHandler(audio.ledger, audio.background, acquisition.NewReacquireAdmission(cooldowns))
	}

	return catalogWiring{
		trackRepo:         audio.trackRepo,
		audioStore:        audio.audioStore,
		orphanedAudio:     svc.orphanedAudio,
		setTrackNumberSvc: svc.setTrackNumberSvc,
		trackHandler:      catalog.NewTrackHandler(svc.addTrackSvc, svc.listTracksSvc, svc.getTrackStatusSvc, svc.deleteTrackSvc, svc.setTrackNumberSvc, featuredArtistHandler),
		libraryHandler:    catalog.NewLibraryHandler(catalog.NewLibraryLensService(svc.catalogTrackRepo)),
		playlistHandler:   catalog.NewPlaylistHandler(svc.playlistLifecycleSvc, svc.playlistMembershipSvc),
		streamHandler:     catalog.NewStreamHandler(svc.streamTrackSvc),
		audioURLHandler:   catalog.NewAudioURLHandler(svc.audioURLSvc, catalog.WithPrefetchEnabled(a.cfg.AudioPrefetchEnabled)),
		retryH:            retryH,
		reacquireH:        reacquireH,
		ytDlpSearcher:     audio.ytDlpSearcher,
		ytDlpAvailable:    audio.ytDlpAvailable,
		ytmCanary:         audio.ytmCanary,
		scheduler:         audio.background,
	}
}

type sourceTiers struct {
	first    []acquisition.AudioSource
	fallback []acquisition.AudioSource
}

func (t sourceTiers) all() []acquisition.AudioSource {
	return append(append([]acquisition.AudioSource{}, t.first...), t.fallback...)
}

func (a *App) acquisitionSources(ctx context.Context) (sourceTiers, acquisition.AcquisitionVerification) {
	if a.cfg.AcquisitionFixtureEnabled() {
		slog.WarnContext(ctx, "acquisition: fixture audio source replaces every live source", "env", a.cfg.Env)
		return sourceTiers{first: []acquisition.AudioSource{acquisition.NewArtTrackSource()}, fallback: []acquisition.AudioSource{acquisition.NewUploadSource()}}, acquisition.AcquisitionVerification{}
	}
	searcher := a.newYtDlpSearcher()
	tools := acquisition.AcquisitionVerification{YtDlp: searcher.Available()}
	return a.audioSourcesFor(ctx, searcher), tools
}

func (a *App) innerTubeClient(ctx context.Context) *http.Client {
	settings := ytminnertube.Settings{MinInterval: a.cfg.YtmInnerTubeMinInterval, Cooldown: a.cfg.YtmInnerTubeCooldown}
	budget, err := ytminnertube.NewBudget(a.redisClient, settings)
	if err != nil {
		slog.WarnContext(ctx, "acquisition: no shared InnerTube budget without redis, pacing in-process only")
		return nil
	}
	return &http.Client{Transport: budget.Transport(nil)}
}

func (a *App) sourceCanarySearcher() *acquisition.YtDlpAudioSearcher {
	if a.cfg.AcquisitionFixtureEnabled() {
		return nil
	}
	return a.newYtDlpSearcher()
}

func (a *App) sourceCanaryYTMClient() *acquisition.YtmSearchClient {
	if a.cfg.AcquisitionFixtureEnabled() {
		return nil
	}
	return a.budgetedYTMSearchClient(context.Background())
}

func newYTMSearchClient() *acquisition.YtmSearchClient {
	return acquisition.NewYtmSearch(acquisition.YtmSearchConfig{MinInterval: ytmSearchMinInterval, Timeout: ytmSearchTimeout})
}

func (a *App) budgetedYTMSearchClient(ctx context.Context) *acquisition.YtmSearchClient {
	return acquisition.NewYtmSearch(acquisition.YtmSearchConfig{HTTP: a.innerTubeClient(ctx), MinInterval: ytmSearchMinInterval, Timeout: ytmSearchTimeout})
}

func (a *App) newYtDlpSearcher() *acquisition.YtDlpAudioSearcher {
	return acquisition.NewYtDlpAudioSearcher(a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime).
		WithRemoteComponents(a.cfg.YtDLPRemoteComponents).
		WithPOTokenURL(a.cfg.YtDLPPOTokenURL)
}

func (a *App) audioSourcesFor(ctx context.Context, searcher *acquisition.YtDlpAudioSearcher) sourceTiers {
	var tiers sourceTiers
	if a.cfg.YtMusicEnabled {
		songs := a.budgetedYTMSearchClient(ctx)
		tiers.first = append(tiers.first, acquisition.NewYtmSearchSource(songs, searcher), acquisition.NewYtMusicSource(searcher, songs))
	} else {
		slog.InfoContext(ctx, "acquisition: ytmusic source disabled via YTMUSIC_ENABLED")
	}
	if a.cfg.YtDLPEnabled {
		tiers.fallback = append(tiers.fallback, acquisition.NewYtDlpSource(searcher))
	} else {
		slog.InfoContext(ctx, "acquisition: yt-dlp source disabled via YTDLP_ENABLED")
	}
	if len(tiers.all()) > 0 {
		tiers.first = append([]acquisition.AudioSource{acquisition.NewSource(searcher, acquisition.WithPageReader(acquisition.NewHTTPPageReader(sharedLiveTransport(), fileHostPageTimeout)))}, tiers.first...)
	}
	return tiers
}

const fileHostPageTimeout = 10 * time.Second

const (
	ytmSearchMinInterval = 200 * time.Millisecond
	ytmSearchTimeout     = 10 * time.Second
)

func newOwnershipEnrichment(cat catalogWiring) *discovery.OwnershipEnrichmentService {
	return discovery.NewOwnershipEnrichmentService(
		discovery.NewOwnershipReader(cat.trackRepo),
		discovery.NewTrackNumberWriter(cat.setTrackNumberSvc),
	)
}
