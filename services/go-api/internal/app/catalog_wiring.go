package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/acquisition/adapters/chromaprint"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/service/ownership"
	"altune/go-api/internal/observe/service/eventtap"
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

	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"

	catalogHandler "altune/go-api/internal/catalog/adapters/handler"

	catalogPorts "altune/go-api/internal/catalog/ports"
	audioservice "altune/go-api/internal/catalog/service/audio"
	featuredservice "altune/go-api/internal/catalog/service/featured"
	libraryservice "altune/go-api/internal/catalog/service/library"
	playlistservice "altune/go-api/internal/catalog/service/playlist"
	trackservice "altune/go-api/internal/catalog/service/track"
)

type catalogWiring struct {
	trackRepo         *catalog.PgxTrackRepository
	audioStore        catalogPorts.AudioStore
	orphanedAudio     *catalog.PgxOrphanedAudioRepository
	setTrackNumberSvc *trackservice.SetTrackNumberService
	trackHandler      *catalogHandler.TrackHandler
	libraryHandler    *catalogHandler.LibraryHandler
	playlistHandler   *catalogHandler.PlaylistHandler
	streamHandler     *catalogHandler.StreamHandler
	audioURLHandler   *catalogHandler.AudioURLHandler
	retryH            *acquisition.RetryHandler
	reacquireH        *acquisition.ReacquireHandler
	ytDlpSearcher     *ytdlp.YtDlpAudioSearcher
	ytDlpAvailable    bool
	ytmCanary         *ytmsearch.Client
	scheduler         *acqService.BackgroundAcquisitionScheduler
}

type audioSourcesStaging struct {
	audioStore     catalogPorts.AudioStore
	trackRepo      *catalog.PgxTrackRepository
	ledger         *acquisition.TrackLedger
	scheduler      catalogPorts.AcquisitionScheduler
	background     *acqService.BackgroundAcquisitionScheduler
	ytDlpSearcher  *ytdlp.YtDlpAudioSearcher
	ytDlpAvailable bool
	ytmCanary      *ytmsearch.Client
}

type catalogServicesStaging struct {
	catalogTrackRepo      *catalog.PgxCatalogTrackRepository
	orphanedAudio         *catalog.PgxOrphanedAudioRepository
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
	acqService.SweepStaleTempDirs(acquisition.NewTempWorkspace())

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
	audioStore, err := catalog.FromConfig(a.cfg, slog.Default())
	if err != nil {
		return audioSourcesStaging{}, err
	}
	trackRepo := catalog.NewPgxTrackRepository(a.pool)

	var audioSources sourceTiers
	var tools acqPorts.AcquisitionVerification
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
	tap *eventtap.Tap,
	disc discoveryWiring,
	ledger *acquisition.TrackLedger,
	audioStore catalogPorts.AudioStore,
	audioSources sourceTiers,
	verification acqPorts.AcquisitionVerification,
) *acqService.BackgroundAcquisitionScheduler {
	skips := &acqService.VerifySkipCounter{}
	audioProber := ytdlp.NewFfprobeProber(a.cfg.FFmpegLocation, ytdlp.WithSkipRecorder(skips))
	verification.Ffprobe, verification.Ffmpeg = audioProber.Available()

	acquireOpts := []func(*acqService.AcquireTrackAudioService){
		acqService.WithAcquireEvents(tap),
		acqService.WithAcquireOrphanQueue(acquisition.NewOrphanedAudioRecorder(catalog.NewPgxOrphanedAudioRepository(a.pool))),
		acqService.WithSupersededAudioGrace(a.cfg.AcquisitionSupersededAudioGrace),
		acqService.WithAudioProber(audioProber),
		acqService.WithVerifySkips(skips),
		acqService.WithAudioTagger(acquisition.NewFFmpegTagger(binpath.Resolve("ffmpeg", a.cfg.FFmpegLocation), acquisition.NewId3Tagger())),
		acqService.WithAcquireStoreKeyPrefix(a.cfg.AudioKeyPrefix),
		acqService.WithRejectionStore(acquisition.NewPgxRejectionStore(a.pool)),
	}
	if disc.searchSvc != nil {
		var resolverOpts []func(*acquisition.RecordingResolver)
		if disc.musicBrainz != nil {
			resolverOpts = append(resolverOpts,
				acquisition.WithISRCAuthority(disc.musicBrainz),
				acquisition.WithRerecordingAuthority(disc.musicBrainz),
				acquisition.WithRecordingLookup(acquisition.NewRecordingLookup(disc.musicBrainz)))
		}
		recordingResolver := acquisition.NewRecordingResolver(disc.searchSvc, resolverOpts...)
		acquireOpts = append(acquireOpts, acqService.WithRecordingResolver(recordingResolver))
		if disc.musicBrainz != nil {
			acquireOpts = append(acquireOpts,
				acqService.WithAcquireRecordingLookup(recordingResolver),
				acqService.WithRerecordingFinder(recordingResolver))
		}
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
		acqService.WithAcquireTempWorkspace(acquisition.NewTempWorkspace()),
		acqService.WithStoredAudioReader(storedAudioReader{store: audioStore}))
	if a.upgradeEnabled() {
		acquireOpts = append(acquireOpts, acqService.WithPreviewMatcher(a.upgradePreviewMatcher(), a.cfg.AcquisitionUpgradePreviewAMaxBER))
	}
	acquireSvc := acqService.NewAcquireTrackAudioService(
		ledger,
		acqService.NewSourceRegistry(audioSources.first...).WithFallback(audioSources.fallback...),
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
	if a.upgradeEnabled() {
		schedulerOpts = append(schedulerOpts, a.upgradeLane(ledger, acquireSvc))
	}
	if a.pool != nil {
		schedulerOpts = append(schedulerOpts,
			acqService.WithJobQueue(acquisition.NewPgxJobQueue(a.pool)),
			acqService.WithJobNotifier(acquisition.NewPgxJobNotifier(a.pool)),
			acqService.WithOutcomeRecorder(acquisition.NewPgxOutcomeStore(a.pool)))
	}
	return acqService.NewBackgroundAcquisitionScheduler(acquireSvc, &a.wg, a.sem, schedulerOpts...)
}

type storedAudioReader struct {
	store catalogPorts.AudioStore
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
	acqPorts.AudioIdentifier
	succeeded atomic.Bool
}

func (t *lookupTracker) Identify(ctx context.Context, filePath string, fullLengthForPreview float64) (acqPorts.RecordingMatch, error) {
	match, err := t.AudioIdentifier.Identify(ctx, filePath, fullLengthForPreview)
	switch {
	case err == nil:
		t.succeeded.Store(true)
	case !errors.Is(err, acqdomain.ErrIdentifyThrottled):
		t.succeeded.Store(false)
	}
	return match, err
}

func (t *lookupTracker) Verified() bool { return t.succeeded.Load() }

func (a *App) wireCatalogServices(
	tap *eventtap.Tap,
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

	var retryH *acquisition.RetryHandler
	var reacquireH *acquisition.ReacquireHandler
	if audio.scheduler != nil {
		cooldowns := acquisition.NewPgxCooldownStore(a.pool)
		retryH = acquisition.NewRetryHandler(audio.ledger, audio.scheduler, acqService.NewRetryAdmission(cooldowns))
		reacquireH = acquisition.NewReacquireHandler(audio.ledger, audio.background, acqService.NewReacquireAdmission(cooldowns))
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
		ytmCanary:         audio.ytmCanary,
		scheduler:         audio.background,
	}
}

type sourceTiers struct {
	first    []acqPorts.AudioSource
	fallback []acqPorts.AudioSource
}

func (t sourceTiers) all() []acqPorts.AudioSource {
	return append(append([]acqPorts.AudioSource{}, t.first...), t.fallback...)
}

func (a *App) acquisitionSources(ctx context.Context) (sourceTiers, acqPorts.AcquisitionVerification) {
	if a.cfg.AcquisitionFixtureEnabled() {
		slog.WarnContext(ctx, "acquisition: fixture audio source replaces every live source", "env", a.cfg.Env)
		return sourceTiers{first: []acqPorts.AudioSource{acquisition.NewArtTrackSource()}, fallback: []acqPorts.AudioSource{acquisition.NewUploadSource()}}, acqPorts.AcquisitionVerification{}
	}
	searcher := a.newYtDlpSearcher()
	tools := acqPorts.AcquisitionVerification{YtDlp: searcher.Available()}
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

func (a *App) sourceCanarySearcher() *ytdlp.YtDlpAudioSearcher {
	if a.cfg.AcquisitionFixtureEnabled() {
		return nil
	}
	return a.newYtDlpSearcher()
}

func (a *App) sourceCanaryYTMClient() *ytmsearch.Client {
	if a.cfg.AcquisitionFixtureEnabled() {
		return nil
	}
	return a.budgetedYTMSearchClient(context.Background())
}

func newYTMSearchClient() *ytmsearch.Client {
	return ytmsearch.New(ytmsearch.Config{MinInterval: ytmSearchMinInterval, Timeout: ytmSearchTimeout})
}

func (a *App) budgetedYTMSearchClient(ctx context.Context) *ytmsearch.Client {
	return ytmsearch.New(ytmsearch.Config{HTTP: a.innerTubeClient(ctx), MinInterval: ytmSearchMinInterval, Timeout: ytmSearchTimeout})
}

func (a *App) newYtDlpSearcher() *ytdlp.YtDlpAudioSearcher {
	return ytdlp.NewYtDlpAudioSearcher(a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime).
		WithRemoteComponents(a.cfg.YtDLPRemoteComponents).
		WithPOTokenURL(a.cfg.YtDLPPOTokenURL)
}

func (a *App) audioSourcesFor(ctx context.Context, searcher *ytdlp.YtDlpAudioSearcher) sourceTiers {
	var tiers sourceTiers
	if a.cfg.YtMusicEnabled {
		songs := a.budgetedYTMSearchClient(ctx)
		tiers.first = append(tiers.first, ytmsearch.NewSource(songs, searcher), ytmusic.NewSource(searcher, songs))
	} else {
		slog.InfoContext(ctx, "acquisition: ytmusic source disabled via YTMUSIC_ENABLED")
	}
	if a.cfg.YtDLPEnabled {
		tiers.fallback = append(tiers.fallback, ytdlp.NewSource(searcher))
	} else {
		slog.InfoContext(ctx, "acquisition: yt-dlp source disabled via YTDLP_ENABLED")
	}
	if len(tiers.all()) > 0 {
		tiers.first = append([]acqPorts.AudioSource{acquisition.NewSource(searcher, acquisition.WithPageReader(acquisition.NewHTTPPageReader(sharedLiveTransport(), fileHostPageTimeout)))}, tiers.first...)
	}
	return tiers
}

const fileHostPageTimeout = 10 * time.Second

const (
	ytmSearchMinInterval = 200 * time.Millisecond
	ytmSearchTimeout     = 10 * time.Second
)

func newOwnershipEnrichment(cat catalogWiring) *ownership.OwnershipEnrichmentService {
	return ownership.NewOwnershipEnrichmentService(
		discovery.NewOwnershipReader(cat.trackRepo),
		discovery.NewTrackNumberWriter(cat.setTrackNumberSvc),
	)
}
