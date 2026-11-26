package app

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/app/bridges/libraryfromsearch"
	"altune/go-api/internal/app/bridges/searchfromlibrary"
	"altune/go-api/internal/app/downloadsetup"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type catalogWiring struct {
	trackRepo         *library.PostgresTrackRepository
	audioStore        library.AudioStore
	orphanedAudio     *library.PostgresOrphanedAudioRepository
	setTrackNumberSvc *library.SetTrackNumberService
	trackHandler      *library.TrackHandler
	libraryHandler    *library.LibraryHandler
	playlistHandler   *library.PlaylistHandler
	streamHandler     *library.StreamHandler
	audioURLHandler   *library.AudioURLHandler
	retryH            *download.RetryHandler
	reacquireH        *download.ReacquireHandler
	acquisitionOn     bool
	ytDlpSearcher     *download.YouTubeAudioSearcher
	ytDlpAvailable    bool
	ytmCanary         *download.YouTubeMusicSearchClient
	scheduler         *download.BackgroundAcquisitionScheduler
}

type audioSourcesStaging struct {
	audioStore     library.AudioStore
	trackRepo      *library.PostgresTrackRepository
	ledger         download.TrackLedgerPort
	scheduler      library.AcquisitionScheduler
	acquisitionOn  bool
	background     *download.BackgroundAcquisitionScheduler
	ytDlpSearcher  *download.YouTubeAudioSearcher
	ytDlpAvailable bool
	ytmCanary      *download.YouTubeMusicSearchClient
}

type catalogServicesStaging struct {
	catalogTrackRepo      *library.PostgresCatalogTrackRepository
	orphanedAudio         *library.PostgresOrphanedAudioRepository
	addTrackSvc           *library.AddTrackService
	listTracksSvc         *library.ListTracksService
	deleteTrackSvc        *library.DeleteTrackService
	setTrackNumberSvc     *library.SetTrackNumberService
	playlistLifecycleSvc  *library.PlaylistLifecycleService
	playlistMembershipSvc *library.PlaylistMembershipService
	backfillFeaturedSvc   *library.FillFeaturedArtistsService
	listFeaturingSvc      *library.ListFeaturingService
	getTrackStatusSvc     *library.GetTrackStatusService
	streamTrackSvc        *library.StreamTrackService
	audioURLSvc           *library.AudioLinkService
}

func (a *App) wireCatalog(
	ctx context.Context,
	transport http.RoundTripper,
	tap *monitoring.EventTap,
	disc *search.Discovery,
) (catalogWiring, error) {
	download.SweepStaleTempDirs(download.NewTempWorkspace(), time.Now)

	audio, err := a.wireAudioSources(ctx, transport, tap, disc)
	if err != nil {
		return catalogWiring{}, err
	}
	services, err := a.wireCatalogServices(tap, featuredBridge(disc), audio)
	if err != nil {
		return catalogWiring{}, err
	}
	return a.wireCatalogHandlers(audio, services), nil
}

func featuredBridge(disc *search.Discovery) library.FeaturedArtistResolver {
	if disc.FeaturedArtists == nil {
		return nil
	}
	return library.NewFeaturedArtistResolver(libraryfromsearch.NewFeaturedArtistCredits(disc.FeaturedArtists))
}

func (a *App) wireAudioSources(
	ctx context.Context,
	transport http.RoundTripper,
	tap *monitoring.EventTap,
	disc *search.Discovery,
) (audioSourcesStaging, error) {
	audioStore, err := library.NewAudioStoreFromConfig(a.cfg, slog.Default())
	if err != nil {
		return audioSourcesStaging{}, err
	}
	trackRepo := library.NewPostgresTrackRepository(a.pool)
	sources, tools, err := a.liveTiers(ctx, transport, audioStore)
	if err != nil {
		return audioSourcesStaging{}, err
	}
	ytmCanary, err := downloadsetup.CanaryYTMClient(a.cfg, a.redisClient, transport)
	if err != nil {
		return audioSourcesStaging{}, fmt.Errorf("build the ytmusic canary client: %w", err)
	}
	staging := audioSourcesStaging{
		audioStore:     audioStore,
		trackRepo:      trackRepo,
		ledger:         downloadfromlibrary.NewLibraryTracks(trackRepo, time.Now),
		ytDlpSearcher:  downloadsetup.YtDlpSearcher(a.cfg),
		ytDlpAvailable: tools.YtDlp,
		ytmCanary:      ytmCanary,
	}
	if len(sources.All()) > 0 && audioStore != nil {
		staging.background = downloadsetup.NewScheduler(ctx, a.schedulerDeps(transport, tap, disc, staging, sources, tools))
		staging.scheduler = staging.background
		staging.acquisitionOn = true
	}
	return staging, nil
}

func (a *App) liveTiers(ctx context.Context, transport http.RoundTripper, audioStore library.AudioStore) (downloadsetup.SourceOrder, download.Verification, error) {
	if audioStore == nil {
		return downloadsetup.SourceOrder{}, download.Verification{}, nil
	}
	return downloadsetup.EnabledSources(ctx, a.cfg, a.redisClient, transport)
}

func (a *App) schedulerDeps(transport http.RoundTripper, tap *monitoring.EventTap, disc *search.Discovery, staging audioSourcesStaging, sources downloadsetup.SourceOrder, tools download.Verification) downloadsetup.Deps {
	return downloadsetup.Deps{
		Config: a.cfg, Pool: a.pool, Transport: transport, Events: tap, Recording: downloadfromsearch.RecordingOptions(disc), Ledger: staging.ledger, AudioStore: staging.audioStore,
		Sources: sources, Verification: tools,
	}
}

func (a *App) wireCatalogServices(
	tap *monitoring.EventTap,
	featuredBridge library.FeaturedArtistResolver,
	audio audioSourcesStaging,
) (catalogServicesStaging, error) {
	catalogTrackRepo := library.NewPostgresCatalogTrackRepository(a.pool)
	playlistRepo := library.NewPostgresPlaylistRepository(a.pool)
	orphanedAudio := library.NewPostgresOrphanedAudioRepository(a.pool)
	audioStoreMetrics := library.NewExpvarAudioStoreMetrics()
	library.SetDBCallMetrics(library.NewExpvarDBCallMetrics())

	addTrackSvc, err := library.NewAddTrackService(catalogTrackRepo, tap, addTrackScheduler(audio.scheduler), time.Now)
	if err != nil {
		return catalogServicesStaging{}, fmt.Errorf("build the add-track service: %w", err)
	}
	playlistLifecycleSvc, err := library.NewPlaylistLifecycleService(playlistRepo, tap, time.Now)
	if err != nil {
		return catalogServicesStaging{}, fmt.Errorf("build the playlist lifecycle service: %w", err)
	}
	playlistMembershipSvc, err := library.NewPlaylistMembershipService(playlistRepo, catalogTrackRepo, tap, time.Now)
	if err != nil {
		return catalogServicesStaging{}, fmt.Errorf("build the playlist membership service: %w", err)
	}

	return catalogServicesStaging{
		catalogTrackRepo:      catalogTrackRepo,
		orphanedAudio:         orphanedAudio,
		addTrackSvc:           addTrackSvc,
		listTracksSvc:         library.NewListTracksService(catalogTrackRepo),
		deleteTrackSvc:        library.NewDeleteTrackService(catalogTrackRepo, audio.audioStore, tap, audioStoreMetrics, orphanedAudio),
		setTrackNumberSvc:     library.NewSetTrackNumberService(catalogTrackRepo),
		playlistLifecycleSvc:  playlistLifecycleSvc,
		playlistMembershipSvc: playlistMembershipSvc,
		backfillFeaturedSvc:   library.MustNewBackfillFeaturedService(catalogTrackRepo, catalogTrackRepo, featuredBridge, time.Now),
		listFeaturingSvc:      library.NewListFeaturingService(catalogTrackRepo),
		getTrackStatusSvc:     library.NewGetTrackStatusService(catalogTrackRepo),
		streamTrackSvc:        library.NewStreamTrackService(catalogTrackRepo, audio.audioStore, addTrackScheduler(audio.scheduler), audioStoreMetrics, a.jobs.Switch(jobs.StreamRecovery)),
		audioURLSvc:           library.MustNewAudioURLService(catalogTrackRepo, audio.audioStore, time.Now, audioStoreMetrics),
	}, nil
}

func addTrackScheduler(scheduler library.AcquisitionScheduler) library.AcquisitionScheduler {
	if scheduler == nil {
		return library.NoopAcquisitionScheduler()
	}
	return scheduler
}

func (a *App) wireCatalogHandlers(audio audioSourcesStaging, svc catalogServicesStaging) catalogWiring {
	featuredArtistHandler := library.NewFeaturedArtistHandler(svc.backfillFeaturedSvc, svc.listFeaturingSvc)

	var retryH *download.RetryHandler
	var reacquireH *download.ReacquireHandler
	if audio.acquisitionOn {
		cooldowns := download.NewPostgresCooldownStore(a.pool)
		retryH = download.NewRetryHandler(audio.ledger, audio.scheduler, download.NewRetryAdmission(cooldowns))
		reacquireH = download.NewReacquireHandler(audio.ledger, audio.background, download.NewReacquireAdmission(cooldowns))
	}

	return catalogWiring{
		trackRepo:         audio.trackRepo,
		audioStore:        audio.audioStore,
		orphanedAudio:     svc.orphanedAudio,
		setTrackNumberSvc: svc.setTrackNumberSvc,
		trackHandler:      library.MustNewTrackHandler(svc.addTrackSvc, svc.listTracksSvc, svc.getTrackStatusSvc, svc.deleteTrackSvc, svc.setTrackNumberSvc, featuredArtistHandler, time.Now),
		libraryHandler:    library.NewLibraryHandler(library.NewLibraryLensService(svc.catalogTrackRepo)),
		playlistHandler:   library.MustNewPlaylistHandler(svc.playlistLifecycleSvc, svc.playlistMembershipSvc, time.Now),
		streamHandler:     library.MustNewStreamHandler(svc.streamTrackSvc, time.Now),
		audioURLHandler:   library.MustNewAudioURLHandler(svc.audioURLSvc, time.Now, library.WithPrefetchEnabled(a.cfg.AudioPrefetchEnabled)),
		retryH:            retryH,
		reacquireH:        reacquireH,
		acquisitionOn:     audio.acquisitionOn,
		ytDlpSearcher:     audio.ytDlpSearcher,
		ytDlpAvailable:    audio.ytDlpAvailable,
		ytmCanary:         audio.ytmCanary,
		scheduler:         audio.background,
	}
}

func newOwnershipEnrichment(cat catalogWiring, bg *background.Tracker) (*search.OwnershipEnrichmentService, error) {
	return search.NewOwnershipEnrichmentService(
		bg,
		search.NewOwnershipReader(searchfromlibrary.NewOwnedTracks(cat.trackRepo)),
		searchfromlibrary.NewTrackNumberFiller(cat.setTrackNumberSvc),
	)
}
