package app

import (
	"altune/go-api/internal/acquisition/adapters/chromaprint"
	"altune/go-api/internal/acquisition/adapters/id3"
	"altune/go-api/internal/acquisition/adapters/streamrip"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/catalog/adapters/discoverybridge"
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/shared/config"
	"fmt"
	"log/slog"
	"strings"

	acqDiscoveryBridge "altune/go-api/internal/acquisition/adapters/discoverybridge"
	acqHandler "altune/go-api/internal/acquisition/adapters/handler"
	acqPersistence "altune/go-api/internal/acquisition/adapters/persistence"

	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"

	catalogHandler "altune/go-api/internal/catalog/adapters/handler"
	catalogMetrics "altune/go-api/internal/catalog/adapters/metrics"

	catalogPorts "altune/go-api/internal/catalog/ports"
	catalogService "altune/go-api/internal/catalog/service"

	discoveryService "altune/go-api/internal/discovery/service"
)

type catalogWiring struct {
	trackRepo         *persistence.PgxTrackRepository
	audioStore        catalogPorts.AudioStore
	orphanedAudio     *persistence.PgxOrphanedAudioRepository
	setTrackNumberSvc *catalogService.SetTrackNumberService
	trackHandler      *catalogHandler.TrackHandler
	libraryHandler    *catalogHandler.LibraryHandler
	playlistHandler   *catalogHandler.PlaylistHandler
	streamHandler     *catalogHandler.StreamHandler
	audioURLHandler   *catalogHandler.AudioURLHandler
	retryH            *acqHandler.RetryHandler
	reacquireH        *acqHandler.ReacquireHandler
}

// audioSourcesStaging carries the audio store, the acquisition track repository
// and the (possibly nil) acquisition scheduler from wireAudioSources to the
// catalog service and handler wiring steps.
type audioSourcesStaging struct {
	audioStore catalogPorts.AudioStore
	trackRepo  *persistence.PgxTrackRepository
	scheduler  catalogPorts.AcquisitionScheduler
}

// catalogServicesStaging carries the catalog services and the catalog track
// repository from wireCatalogServices to wireCatalogHandlers.
type catalogServicesStaging struct {
	catalogTrackRepo      *persistence.PgxCatalogTrackRepository
	orphanedAudio         *persistence.PgxOrphanedAudioRepository
	addTrackSvc           *catalogService.AddTrackService
	listTracksSvc         *catalogService.ListTracksService
	deleteTrackSvc        *catalogService.DeleteTrackService
	setTrackNumberSvc     *catalogService.SetTrackNumberService
	playlistLifecycleSvc  *catalogService.PlaylistLifecycleService
	playlistMembershipSvc *catalogService.PlaylistMembershipService
	backfillFeaturedSvc   *catalogService.BackfillFeaturedService
	listFeaturingSvc      *catalogService.ListFeaturingService
	getTrackStatusSvc     *catalogService.GetTrackStatusService
	streamTrackSvc        *catalogService.StreamTrackService
	audioURLSvc           *catalogService.AudioURLService
}

func (a *App) wireCatalog(
	tap *eventtap.Tap,
	featuredBridge *discoverybridge.FeaturedResolver,
	searchSvc *discoveryService.Service,
) (catalogWiring, error) {
	// Reap the temp dirs a hard-killed predecessor leaked before any scheduler
	// exists to create new ones, so a dir older than a job's own deadline is
	// provably abandoned rather than merely idle (#1978).
	acqService.SweepStaleTempDirs()

	audio, err := a.wireAudioSources(tap, searchSvc)
	if err != nil {
		return catalogWiring{}, err
	}
	services := a.wireCatalogServices(tap, featuredBridge, audio)
	return a.wireCatalogHandlers(audio, services), nil
}

// wireAudioSources builds the audio store, the enabled acquisition sources and,
// when both exist, the background acquisition scheduler with its verification
// status. The scheduler is also recorded on the App for shutdown.
func (a *App) wireAudioSources(
	tap *eventtap.Tap,
	searchSvc *discoveryService.Service,
) (audioSourcesStaging, error) {
	audioStore, err := a.buildAudioStore()
	if err != nil {
		return audioSourcesStaging{}, err
	}
	trackRepo := persistence.NewPgxTrackRepository(a.pool)

	var audioSources []acqPorts.AudioSource
	var tools acqPorts.AcquisitionVerification
	if audioStore != nil {
		searcher := ytdlp.NewYtDlpAudioSearcher(
			a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime)
		tools.YtDlp = searcher.Available()
		audioSources, tools.Streamrip = a.audioSourcesFor(searcher)
	}

	staging := audioSourcesStaging{audioStore: audioStore, trackRepo: trackRepo}
	if len(audioSources) > 0 && audioStore != nil {
		bgScheduler := a.buildAcquisitionScheduler(tap, searchSvc, trackRepo, audioStore, audioSources, tools)
		a.scheduler = bgScheduler
		staging.scheduler = bgScheduler
	}
	return staging, nil
}

// buildAcquisitionScheduler assembles the acquire service (prober, tagger,
// optional recording resolver and fingerprint identifier) and wraps it in the
// background scheduler that reports the resulting verification status. The
// passed verification carries the source-level probes (yt-dlp, streamrip); the
// ffprobe, ffmpeg and fpcalc probes are filled in here.
func (a *App) buildAcquisitionScheduler(
	tap *eventtap.Tap,
	searchSvc *discoveryService.Service,
	trackRepo *persistence.PgxTrackRepository,
	audioStore catalogPorts.AudioStore,
	audioSources []acqPorts.AudioSource,
	verification acqPorts.AcquisitionVerification,
) *acqService.BackgroundAcquisitionScheduler {
	audioProber := ytdlp.NewFfprobeProber(a.cfg.FFmpegLocation)
	verification.Ffprobe, verification.Ffmpeg = audioProber.Available()

	acquireOpts := []func(*acqService.AcquireTrackAudioService){
		acqService.WithAcquireEvents(tap),
		acqService.WithAcquireOrphanQueue(persistence.NewPgxOrphanedAudioRepository(a.pool)),
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
	return acqService.NewBackgroundAcquisitionScheduler(acquireSvc, &a.wg, a.sem,
		acqService.WithSchedulerEvents(tap),
		acqService.WithPrincipalQueueDepth(a.principalQueueDepth()),
		acqService.WithVerificationStatus(verification))
}

// principalQueueDepth is the per-principal fair-share cap wired into production
// (#1418), turning #964's default-off gate on. It honors
// ACQUISITION_PRINCIPAL_QUEUE_DEPTH and, when unset/non-positive, defaults to
// the worker concurrency: one user may keep every worker busy but not fill the
// deeper global queue, leaving room for other principals.
func (a *App) principalQueueDepth() int {
	if depth := a.cfg.AcquisitionPrincipalQueueDepth; depth > 0 {
		return depth
	}
	return a.cfg.AcquisitionConcurrency
}

// wireCatalogServices constructs the catalog application services over the
// catalog and playlist repositories, the audio store and the scheduler.
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
		addTrackSvc: catalogService.NewAddTrackService(
			catalogTrackRepo,
			catalogService.WithAddTrackEvents(tap),
			catalogService.WithAcquisitionScheduler(audio.scheduler),
		),
		listTracksSvc:         catalogService.NewListTracksService(catalogTrackRepo),
		deleteTrackSvc:        catalogService.NewDeleteTrackService(catalogTrackRepo, audio.audioStore, catalogService.WithDeleteTrackEvents(tap), catalogService.WithDeleteTrackMetrics(audioStoreMetrics), catalogService.WithDeleteTrackOrphanQueue(orphanedAudio)),
		setTrackNumberSvc:     catalogService.NewSetTrackNumberService(catalogTrackRepo),
		playlistLifecycleSvc:  catalogService.NewPlaylistLifecycleService(playlistRepo, catalogService.WithPlaylistLifecycleEvents(tap)),
		playlistMembershipSvc: catalogService.NewPlaylistMembershipService(playlistRepo, catalogTrackRepo, catalogService.WithPlaylistMembershipEvents(tap)),
		backfillFeaturedSvc:   catalogService.NewBackfillFeaturedService(catalogTrackRepo, catalogTrackRepo, featuredBridge),
		listFeaturingSvc:      catalogService.NewListFeaturingService(catalogTrackRepo),
		getTrackStatusSvc:     catalogService.NewGetTrackStatusService(catalogTrackRepo),
		streamTrackSvc:        catalogService.NewStreamTrackService(catalogTrackRepo, audio.audioStore, catalogService.WithStreamScheduler(audio.scheduler), catalogService.WithStreamMetrics(audioStoreMetrics), catalogService.WithStreamRecoverySwitch(a.jobSwitch(jobStreamRecovery))),
		audioURLSvc:           catalogService.NewAudioURLService(catalogTrackRepo, audio.audioStore, catalogService.WithAudioURLMetrics(audioStoreMetrics)),
	}
}

// wireCatalogHandlers constructs the catalog HTTP handlers and, when an
// acquisition scheduler exists, the retry and reacquire handlers.
func (a *App) wireCatalogHandlers(audio audioSourcesStaging, svc catalogServicesStaging) catalogWiring {
	featuredArtistHandler := catalogHandler.NewFeaturedArtistHandler(svc.backfillFeaturedSvc, svc.listFeaturingSvc)

	var retryH *acqHandler.RetryHandler
	var reacquireH *acqHandler.ReacquireHandler
	if audio.scheduler != nil {
		cooldowns := acqPersistence.NewFallbackCooldownStore(acqPersistence.NewPgxCooldownStore(a.pool))
		retryH = acqHandler.NewRetryHandler(audio.trackRepo, audio.scheduler, acqService.NewRetryAdmission(cooldowns))
		reacquireH = acqHandler.NewReacquireHandler(audio.trackRepo, a.scheduler, acqService.NewReacquireAdmission(cooldowns))
	}

	return catalogWiring{
		trackRepo:         audio.trackRepo,
		audioStore:        audio.audioStore,
		orphanedAudio:     svc.orphanedAudio,
		setTrackNumberSvc: svc.setTrackNumberSvc,
		trackHandler:      catalogHandler.NewTrackHandler(svc.addTrackSvc, svc.listTracksSvc, svc.getTrackStatusSvc, svc.deleteTrackSvc, svc.setTrackNumberSvc, featuredArtistHandler),
		libraryHandler:    catalogHandler.NewLibraryHandler(catalogService.NewLibraryLensService(svc.catalogTrackRepo)),
		playlistHandler:   catalogHandler.NewPlaylistHandler(svc.playlistLifecycleSvc, svc.playlistMembershipSvc),
		streamHandler:     catalogHandler.NewStreamHandler(svc.streamTrackSvc),
		audioURLHandler:   catalogHandler.NewAudioURLHandler(svc.audioURLSvc, catalogHandler.WithPrefetchEnabled(a.cfg.AudioPrefetchEnabled)),
		retryH:            retryH,
		reacquireH:        reacquireH,
	}
}

// audioSourcesFor assembles the enabled acquisition sources. ytmusic and yt-dlp
// are gated by YTMUSIC_ENABLED / YTDLP_ENABLED (both default enabled) so either
// can be pulled at startup without a deploy, mirroring streamrip's per-service
// opt-in. The passed searcher is shared between the ytmusic and yt-dlp sources.
// The bool is the streamrip binary probe from buildStreamripSources.
func (a *App) audioSourcesFor(searcher *ytdlp.YtDlpAudioSearcher) ([]acqPorts.AudioSource, bool) {
	var sources []acqPorts.AudioSource
	if a.cfg.YtMusicEnabled {
		sources = append(sources, ytmusic.NewSource(searcher))
	} else {
		slog.Info("acquisition: ytmusic source disabled via YTMUSIC_ENABLED")
	}
	streamripSources, streamripOK := a.buildStreamripSources()
	sources = append(sources, streamripSources...)
	if a.cfg.YtDLPEnabled {
		sources = append(sources, ytdlp.NewSource(searcher))
	} else {
		slog.Info("acquisition: yt-dlp source disabled via YTDLP_ENABLED")
	}
	return sources, streamripOK
}

// buildStreamripSources builds one source per enabled, supported streamrip
// service and probes the configured rip binary once, so a missing or
// misconfigured binary is logged as degraded at startup rather than surfacing
// only when a background Fetch fails. The bool is true when the binary is
// runnable or when no streamrip source is enabled.
func (a *App) buildStreamripSources() ([]acqPorts.AudioSource, bool) {
	sources := a.streamripSourcesFor(a.cfg.StreamripServices)
	if len(sources) == 0 {
		return sources, true
	}
	probe := streamrip.NewSource("").WithBinary(a.cfg.StreamripBin)
	if !probe.Available() {
		slog.Warn("acquisition: streamrip binary not runnable, streamrip sources degraded",
			"bin", probe.Binary(), "sources", len(sources))
		return sources, false
	}
	return sources, true
}

func (a *App) streamripSourcesFor(services []string) []acqPorts.AudioSource {
	var sources []acqPorts.AudioSource
	for _, service := range services {
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

func (a *App) buildAudioStore() (catalogPorts.AudioStore, error) {
	if a.cfg.HasOCIS3() {
		store, err := storage.NewObjectStorageAudioStore(storage.ObjectStorageConfig{
			Endpoint:  a.cfg.OCIS3Endpoint,
			AccessKey: a.cfg.OCIS3AccessKey,
			SecretKey: a.cfg.OCIS3SecretKey,
			Bucket:    a.cfg.OCIS3Bucket,
			Region:    a.cfg.OCIS3Region,
		})
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

const ociS3ConfigKeys = 4

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
	if len(missing) > 0 && len(missing) < ociS3ConfigKeys {
		return fmt.Errorf(
			"audio store: incomplete OCI S3 configuration, missing %s (set these for object storage, or set MUSIC_DIR for a filesystem store)",
			strings.Join(missing, ", "),
		)
	}
	return fmt.Errorf(
		"audio store: no backend configured; set MUSIC_DIR for a filesystem store, or all of OCI_S3_ENDPOINT, OCI_S3_ACCESS_KEY, OCI_S3_SECRET_KEY, OCI_S3_BUCKET for object storage",
	)
}
