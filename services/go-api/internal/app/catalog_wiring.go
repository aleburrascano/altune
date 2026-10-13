package app

import (
	"altune/go-api/internal/acquisition/adapters/chromaprint"
	"altune/go-api/internal/acquisition/adapters/ffmpegtag"
	"altune/go-api/internal/acquisition/adapters/fixture"
	"altune/go-api/internal/acquisition/adapters/id3"
	"altune/go-api/internal/acquisition/adapters/tempfs"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	"altune/go-api/internal/catalog/adapters/discoverybridge"
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/discovery/service/ownership"
	"altune/go-api/internal/observe/service/eventtap"
	"altune/go-api/internal/shared/binpath"
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	acqCatalogBridge "altune/go-api/internal/acquisition/adapters/catalogbridge"
	acqDiscoveryBridge "altune/go-api/internal/acquisition/adapters/discoverybridge"
	acqHandler "altune/go-api/internal/acquisition/adapters/handler"
	acqPersistence "altune/go-api/internal/acquisition/adapters/persistence"

	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"

	catalogHandler "altune/go-api/internal/catalog/adapters/handler"
	catalogMetrics "altune/go-api/internal/catalog/adapters/metrics"

	catalogPorts "altune/go-api/internal/catalog/ports"
	audioservice "altune/go-api/internal/catalog/service/audio"
	featuredservice "altune/go-api/internal/catalog/service/featured"
	libraryservice "altune/go-api/internal/catalog/service/library"
	playlistservice "altune/go-api/internal/catalog/service/playlist"
	trackservice "altune/go-api/internal/catalog/service/track"

	discoveryCatalogBridge "altune/go-api/internal/discovery/adapters/catalogbridge"
)

type catalogWiring struct {
	trackRepo         *persistence.PgxTrackRepository
	audioStore        catalogPorts.AudioStore
	orphanedAudio     *persistence.PgxOrphanedAudioRepository
	setTrackNumberSvc *trackservice.SetTrackNumberService
	trackHandler      *catalogHandler.TrackHandler
	libraryHandler    *catalogHandler.LibraryHandler
	playlistHandler   *catalogHandler.PlaylistHandler
	streamHandler     *catalogHandler.StreamHandler
	audioURLHandler   *catalogHandler.AudioURLHandler
	retryH            *acqHandler.RetryHandler
	reacquireH        *acqHandler.ReacquireHandler
	ytDlpSearcher     *ytdlp.YtDlpAudioSearcher
	ytDlpAvailable    bool
	scheduler         *acqService.BackgroundAcquisitionScheduler
}

type audioSourcesStaging struct {
	audioStore     catalogPorts.AudioStore
	trackRepo      *persistence.PgxTrackRepository
	scheduler      catalogPorts.AcquisitionScheduler
	background     *acqService.BackgroundAcquisitionScheduler
	ytDlpSearcher  *ytdlp.YtDlpAudioSearcher
	ytDlpAvailable bool
}

type catalogServicesStaging struct {
	catalogTrackRepo      *persistence.PgxCatalogTrackRepository
	orphanedAudio         *persistence.PgxOrphanedAudioRepository
	addTrackSvc           *trackservice.AddTrackService
	listTracksSvc         *trackservice.ListTracksService
	deleteTrackSvc        *trackservice.DeleteTrackService
	setTrackNumberSvc     *trackservice.SetTrackNumberService
	playlistLifecycleSvc  *playlistservice.PlaylistLifecycleService
	playlistMembershipSvc *playlistservice.PlaylistMembershipService
	backfillFeaturedSvc   *featuredservice.BackfillFeaturedService
	listFeaturingSvc      *featuredservice.ListFeaturingService
	getTrackStatusSvc     *trackservice.GetTrackStatusService
	streamTrackSvc        *audioservice.StreamTrackService
	audioURLSvc           *audioservice.AudioURLService
}

func (a *App) wireCatalog(
	ctx context.Context,
	tap *eventtap.Tap,
	disc discoveryWiring,
) (catalogWiring, error) {
	acqService.SweepStaleTempDirs(tempfs.New())

	audio, err := a.wireAudioSources(ctx, tap, disc)
	if err != nil {
		return catalogWiring{}, err
	}
	services := a.wireCatalogServices(tap, disc.featuredBridge, audio)
	return a.wireCatalogHandlers(audio, services), nil
}

func (a *App) wireAudioSources(
	ctx context.Context,
	tap *eventtap.Tap,
	disc discoveryWiring,
) (audioSourcesStaging, error) {
	audioStore, err := storage.FromConfig(a.cfg, slog.Default())
	if err != nil {
		return audioSourcesStaging{}, err
	}
	trackRepo := persistence.NewPgxTrackRepository(a.pool)

	var audioSources []acqPorts.AudioSource
	var tools acqPorts.AcquisitionVerification
	if audioStore != nil {
		audioSources, tools = a.acquisitionSources(ctx)
	}

	staging := audioSourcesStaging{
		audioStore:     audioStore,
		trackRepo:      trackRepo,
		ytDlpSearcher:  a.sourceCanarySearcher(),
		ytDlpAvailable: tools.YtDlp,
	}
	if len(audioSources) > 0 && audioStore != nil {
		bgScheduler := a.buildAcquisitionScheduler(ctx, tap, disc, trackRepo, audioStore, audioSources, tools)
		staging.background = bgScheduler
		staging.scheduler = bgScheduler
	}
	return staging, nil
}

func (a *App) buildAcquisitionScheduler(
	ctx context.Context,
	tap *eventtap.Tap,
	disc discoveryWiring,
	trackRepo *persistence.PgxTrackRepository,
	audioStore catalogPorts.AudioStore,
	audioSources []acqPorts.AudioSource,
	verification acqPorts.AcquisitionVerification,
) *acqService.BackgroundAcquisitionScheduler {
	skips := &acqService.VerifySkipCounter{}
	audioProber := ytdlp.NewFfprobeProber(a.cfg.FFmpegLocation, ytdlp.WithSkipRecorder(skips))
	verification.Ffprobe, verification.Ffmpeg = audioProber.Available()

	acquireOpts := []func(*acqService.AcquireTrackAudioService){
		acqService.WithAcquireEvents(tap),
		acqService.WithAcquireOrphanQueue(acqCatalogBridge.NewOrphanedAudioRecorder(persistence.NewPgxOrphanedAudioRepository(a.pool))),
		acqService.WithAudioProber(audioProber),
		acqService.WithVerifySkips(skips),
		acqService.WithAudioTagger(ffmpegtag.NewTagger(binpath.Resolve("ffmpeg", a.cfg.FFmpegLocation), id3.NewTagger())),
		acqService.WithAcquireStoreKeyPrefix(a.cfg.AudioKeyPrefix),
		acqService.WithRejectionStore(acqPersistence.NewPgxRejectionStore(a.pool)),
	}
	if disc.searchSvc != nil {
		var resolverOpts []func(*acqDiscoveryBridge.RecordingResolver)
		if disc.musicBrainz != nil {
			resolverOpts = append(resolverOpts, acqDiscoveryBridge.WithISRCAuthority(disc.musicBrainz))
		}
		acquireOpts = append(acquireOpts, acqService.WithRecordingResolver(
			acqDiscoveryBridge.NewRecordingResolver(disc.searchSvc, resolverOpts...)))
	}
	lookups := &lookupTracker{}
	if a.cfg.AcoustIDAPIKey != "" {
		identifier := chromaprint.NewIdentifier(a.cfg.FFmpegLocation, a.cfg.AcoustIDAPIKey)
		verification.Fpcalc = identifier.Available()
		lookups.AudioIdentifier = identifier
		acquireOpts = append(acquireOpts, acqService.WithAudioIdentifier(lookups))
		slog.InfoContext(ctx, "acquisition: fingerprint verification wired", "fpcalc", verification.Fpcalc)
	} else {
		slog.WarnContext(ctx, "acquisition: fingerprint verification disabled", "reason", "ACOUSTID_API_KEY is empty")
	}
	acquireOpts = append(acquireOpts,
		acqService.WithDownloadLimiter(acqService.NewDownloadLimiter(a.cfg.AcquisitionDownloadConcurrency)),
		acqService.WithAcquireConfidenceFloor(a.cfg.AcquisitionConfidenceFloor),
		acqService.WithAcquireTempWorkspace(tempfs.New()))
	acquireSvc := acqService.NewAcquireTrackAudioService(
		acqCatalogBridge.NewTrackLedger(trackRepo),
		acqService.NewSourceRegistry(audioSources...),
		audioStore,
		acquireOpts...,
	)
	schedulerOpts := []func(*acqService.BackgroundAcquisitionScheduler){
		acqService.WithSchedulerPaused(a.cfg.AcquisitionPaused),
		acqService.WithSchedulerEvents(tap),
		acqService.WithVerificationStatus(verification),
		acqService.WithVerifySkipCount(skips.Count),
		acqService.WithFingerprintVerified(lookups.Verified),
		acqService.WithDrainBudget(time.Duration(a.cfg.AcquisitionDrainBudgetSeconds) * time.Second),
	}
	if a.pool != nil {
		schedulerOpts = append(schedulerOpts,
			acqService.WithJobQueue(acqPersistence.NewPgxJobQueue(a.pool)),
			acqService.WithJobNotifier(acqPersistence.NewPgxJobNotifier(a.pool)),
			acqService.WithOutcomeRecorder(acqPersistence.NewPgxOutcomeStore(a.pool)))
	}
	return acqService.NewBackgroundAcquisitionScheduler(acquireSvc, &a.wg, a.sem, schedulerOpts...)
}

type lookupTracker struct {
	acqPorts.AudioIdentifier
	succeeded atomic.Bool
}

func (t *lookupTracker) Identify(ctx context.Context, filePath string, durationHint float64) (acqPorts.RecordingMatch, error) {
	match, err := t.AudioIdentifier.Identify(ctx, filePath, durationHint)
	if err == nil {
		t.succeeded.Store(true)
	}
	return match, err
}

func (t *lookupTracker) Verified() bool { return t.succeeded.Load() }

func (a *App) wireCatalogServices(
	tap *eventtap.Tap,
	featuredBridge *discoverybridge.FeaturedResolver,
	audio audioSourcesStaging,
) catalogServicesStaging {
	catalogTrackRepo := persistence.NewPgxCatalogTrackRepository(a.pool)
	playlistRepo := persistence.NewPgxPlaylistRepository(a.pool)
	orphanedAudio := persistence.NewPgxOrphanedAudioRepository(a.pool)
	audioStoreMetrics := catalogMetrics.NewExpvarAudioStoreMetrics()
	persistence.SetDBCallMetrics(catalogMetrics.NewExpvarDBCallMetrics())

	return catalogServicesStaging{
		catalogTrackRepo: catalogTrackRepo,
		orphanedAudio:    orphanedAudio,
		addTrackSvc: trackservice.NewAddTrackService(
			catalogTrackRepo,
			trackservice.WithAddTrackEvents(tap),
			trackservice.WithAcquisitionScheduler(audio.scheduler),
		),
		listTracksSvc:         trackservice.NewListTracksService(catalogTrackRepo),
		deleteTrackSvc:        trackservice.NewDeleteTrackService(catalogTrackRepo, audio.audioStore, trackservice.WithDeleteTrackEvents(tap), trackservice.WithDeleteTrackMetrics(audioStoreMetrics), trackservice.WithDeleteTrackOrphanQueue(orphanedAudio)),
		setTrackNumberSvc:     trackservice.NewSetTrackNumberService(catalogTrackRepo),
		playlistLifecycleSvc:  playlistservice.NewPlaylistLifecycleService(playlistRepo, playlistservice.WithPlaylistLifecycleEvents(tap)),
		playlistMembershipSvc: playlistservice.NewPlaylistMembershipService(playlistRepo, catalogTrackRepo, playlistservice.WithPlaylistMembershipEvents(tap)),
		backfillFeaturedSvc:   featuredservice.NewBackfillFeaturedService(catalogTrackRepo, catalogTrackRepo, featuredBridge),
		listFeaturingSvc:      featuredservice.NewListFeaturingService(catalogTrackRepo),
		getTrackStatusSvc:     trackservice.NewGetTrackStatusService(catalogTrackRepo),
		streamTrackSvc:        audioservice.NewStreamTrackService(catalogTrackRepo, audio.audioStore, audioservice.WithStreamScheduler(audio.scheduler), audioservice.WithStreamMetrics(audioStoreMetrics), audioservice.WithStreamRecoverySwitch(a.jobSwitch(jobStreamRecovery))),
		audioURLSvc:           audioservice.NewAudioURLService(catalogTrackRepo, audio.audioStore, audioservice.WithAudioURLMetrics(audioStoreMetrics)),
	}
}

func (a *App) wireCatalogHandlers(audio audioSourcesStaging, svc catalogServicesStaging) catalogWiring {
	featuredArtistHandler := catalogHandler.NewFeaturedArtistHandler(svc.backfillFeaturedSvc, svc.listFeaturingSvc)

	var retryH *acqHandler.RetryHandler
	var reacquireH *acqHandler.ReacquireHandler
	if audio.scheduler != nil {
		cooldowns := acqPersistence.NewFallbackCooldownStore(acqPersistence.NewPgxCooldownStore(a.pool))
		retryH = acqHandler.NewRetryHandler(acqCatalogBridge.NewTrackLedger(audio.trackRepo), audio.scheduler, acqService.NewRetryAdmission(cooldowns))
		reacquireH = acqHandler.NewReacquireHandler(acqCatalogBridge.NewTrackLedger(audio.trackRepo), audio.background, acqService.NewReacquireAdmission(cooldowns))
	}

	return catalogWiring{
		trackRepo:         audio.trackRepo,
		audioStore:        audio.audioStore,
		orphanedAudio:     svc.orphanedAudio,
		setTrackNumberSvc: svc.setTrackNumberSvc,
		trackHandler:      catalogHandler.NewTrackHandler(svc.addTrackSvc, svc.listTracksSvc, svc.getTrackStatusSvc, svc.deleteTrackSvc, svc.setTrackNumberSvc, featuredArtistHandler),
		libraryHandler:    catalogHandler.NewLibraryHandler(libraryservice.NewLibraryLensService(svc.catalogTrackRepo)),
		playlistHandler:   catalogHandler.NewPlaylistHandler(svc.playlistLifecycleSvc, svc.playlistMembershipSvc),
		streamHandler:     catalogHandler.NewStreamHandler(svc.streamTrackSvc),
		audioURLHandler:   catalogHandler.NewAudioURLHandler(svc.audioURLSvc, catalogHandler.WithPrefetchEnabled(a.cfg.AudioPrefetchEnabled)),
		retryH:            retryH,
		reacquireH:        reacquireH,
		ytDlpSearcher:     audio.ytDlpSearcher,
		ytDlpAvailable:    audio.ytDlpAvailable,
		scheduler:         audio.background,
	}
}

func (a *App) acquisitionSources(ctx context.Context) ([]acqPorts.AudioSource, acqPorts.AcquisitionVerification) {
	if a.cfg.AcquisitionFixtureEnabled() {
		slog.WarnContext(ctx, "acquisition: fixture audio source replaces every live source", "env", a.cfg.Env)
		return []acqPorts.AudioSource{fixture.NewSource()}, acqPorts.AcquisitionVerification{}
	}
	searcher := a.newYtDlpSearcher()
	tools := acqPorts.AcquisitionVerification{YtDlp: searcher.Available()}
	return a.audioSourcesFor(ctx, searcher), tools
}

func (a *App) sourceCanarySearcher() *ytdlp.YtDlpAudioSearcher {
	if a.cfg.AcquisitionFixtureEnabled() {
		return nil
	}
	return a.newYtDlpSearcher()
}

func (a *App) newYtDlpSearcher() *ytdlp.YtDlpAudioSearcher {
	return ytdlp.NewYtDlpAudioSearcher(a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime).
		WithRemoteComponents(a.cfg.YtDLPRemoteComponents).
		WithPOTokenURL(a.cfg.YtDLPPOTokenURL)
}

func (a *App) audioSourcesFor(ctx context.Context, searcher *ytdlp.YtDlpAudioSearcher) []acqPorts.AudioSource {
	var sources []acqPorts.AudioSource
	if a.cfg.YtMusicEnabled {
		sources = append(sources, ytmusic.NewSource(searcher))
	} else {
		slog.InfoContext(ctx, "acquisition: ytmusic source disabled via YTMUSIC_ENABLED")
	}
	if a.cfg.YtDLPEnabled {
		sources = append(sources, ytdlp.NewSource(searcher))
	} else {
		slog.InfoContext(ctx, "acquisition: yt-dlp source disabled via YTDLP_ENABLED")
	}
	return sources
}

func newOwnershipEnrichment(cat catalogWiring) *ownership.OwnershipEnrichmentService {
	return ownership.NewOwnershipEnrichmentService(
		discoveryCatalogBridge.NewOwnershipReader(cat.trackRepo),
		discoveryCatalogBridge.NewTrackNumberWriter(cat.setTrackNumberSvc),
	)
}
