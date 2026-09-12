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
	ytDlpOK := false
	if audioStore != nil {
		searcher := ytdlp.NewYtDlpAudioSearcher(
			a.cfg.FFmpegLocation, a.cfg.YtDLPCookieFile, a.cfg.YtDLPJSRuntime)
		ytDlpOK = searcher.Available()
		audioSources = a.audioSourcesFor(searcher)
	}

	var scheduler catalogPorts.AcquisitionScheduler
	if len(audioSources) > 0 && audioStore != nil {
		audioProber := ytdlp.NewFfprobeProber(a.cfg.FFmpegLocation)
		ffprobeOK, ffmpegOK := audioProber.Available()
		verification := acqPorts.AcquisitionVerification{
			Ffprobe: ffprobeOK, Ffmpeg: ffmpegOK, YtDlp: ytDlpOK,
		}

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

// audioSourcesFor assembles the enabled acquisition sources. ytmusic and yt-dlp
// are gated by YTMUSIC_ENABLED / YTDLP_ENABLED (both default enabled) so either
// can be pulled at startup without a deploy, mirroring streamrip's per-service
// opt-in. The passed searcher is shared between the ytmusic and yt-dlp sources.
func (a *App) audioSourcesFor(searcher *ytdlp.YtDlpAudioSearcher) []acqPorts.AudioSource {
	var sources []acqPorts.AudioSource
	if a.cfg.YtMusicEnabled {
		sources = append(sources, ytmusic.NewSource(searcher))
	} else {
		slog.Info("acquisition: ytmusic source disabled via YTMUSIC_ENABLED")
	}
	sources = append(sources, a.buildStreamripSources()...)
	if a.cfg.YtDLPEnabled {
		sources = append(sources, ytdlp.NewSource(searcher))
	} else {
		slog.Info("acquisition: yt-dlp source disabled via YTDLP_ENABLED")
	}
	return sources
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
