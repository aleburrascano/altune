package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/acquisitiondiscovery"
	"altune/go-api/internal/app/audioacquisition"
	"altune/go-api/internal/app/catalogdiscovery"
	"altune/go-api/internal/app/discoverycatalog"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/runloop"
	"context"
	"log/slog"
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

func featuredBridge(disc *discovery.Discovery) catalog.FeaturedArtistResolver {
	if disc.FeaturedArtists == nil {
		return nil
	}
	return catalog.NewFeaturedArtistResolver(catalogdiscovery.NewFeaturedArtistCredits(disc.FeaturedArtists))
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
	sources, tools := a.liveTiers(ctx, audioStore)
	staging := audioSourcesStaging{
		audioStore:     audioStore,
		trackRepo:      trackRepo,
		ledger:         acquisition.NewTrackLedger(trackRepo),
		ytDlpSearcher:  audioacquisition.YtDlpSearcher(a.cfg),
		ytDlpAvailable: tools.YtDlp,
		ytmCanary:      audioacquisition.CanaryYTMClient(a.cfg, a.redisClient),
	}
	if len(sources.All()) > 0 && audioStore != nil {
		staging.background = audioacquisition.NewScheduler(ctx, a.schedulerDeps(tap, disc, staging, sources, tools))
		staging.scheduler = staging.background
	}
	return staging, nil
}

func (a *App) liveTiers(ctx context.Context, audioStore catalog.AudioStore) (audioacquisition.Tiers, acquisition.AcquisitionVerification) {
	if audioStore == nil {
		return audioacquisition.Tiers{}, acquisition.AcquisitionVerification{}
	}
	return audioacquisition.LiveTiers(ctx, a.cfg, a.redisClient)
}

func (a *App) schedulerDeps(tap *observe.EventTap, disc *discovery.Discovery, staging audioSourcesStaging, sources audioacquisition.Tiers, tools acquisition.AcquisitionVerification) audioacquisition.Deps {
	return audioacquisition.Deps{
		Config: a.cfg, Pool: a.pool, Events: tap, Recording: acquisitiondiscovery.RecordingOptions(disc), Ledger: staging.ledger, AudioStore: staging.audioStore,
		Sources: sources, Verification: tools, Background: a.jobs.Background(), Slots: a.sem,
	}
}

func (a *App) wireCatalogServices(
	tap *observe.EventTap,
	featuredBridge catalog.FeaturedArtistResolver,
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
		backfillFeaturedSvc:   catalog.NewBackfillFeaturedService(catalogTrackRepo, catalogTrackRepo, featuredBridge, catalog.WithBackfillClock(time.Now)),
		listFeaturingSvc:      catalog.NewListFeaturingService(catalogTrackRepo),
		getTrackStatusSvc:     catalog.NewGetTrackStatusService(catalogTrackRepo),
		streamTrackSvc:        catalog.NewStreamTrackService(catalogTrackRepo, audio.audioStore, catalog.WithStreamScheduler(audio.scheduler), catalog.WithStreamMetrics(audioStoreMetrics), catalog.WithStreamRecoverySwitch(a.jobs.Switch(jobs.StreamRecovery))),
		audioURLSvc: catalog.NewAudioURLService(catalogTrackRepo, audio.audioStore,
			catalog.WithAudioURLMetrics(audioStoreMetrics), catalog.WithAudioURLClock(time.Now)),
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
		trackHandler:      catalog.NewTrackHandler(svc.addTrackSvc, svc.listTracksSvc, svc.getTrackStatusSvc, svc.deleteTrackSvc, svc.setTrackNumberSvc, featuredArtistHandler, catalog.WithTrackWriteLimiterClock(time.Now)),
		libraryHandler:    catalog.NewLibraryHandler(catalog.NewLibraryLensService(svc.catalogTrackRepo)),
		playlistHandler:   catalog.NewPlaylistHandler(svc.playlistLifecycleSvc, svc.playlistMembershipSvc, catalog.WithPlaylistWriteLimiterClock(time.Now)),
		streamHandler:     catalog.NewStreamHandler(svc.streamTrackSvc, catalog.WithStreamLimiterClock(time.Now)),
		audioURLHandler:   catalog.NewAudioURLHandler(svc.audioURLSvc, catalog.WithPrefetchEnabled(a.cfg.AudioPrefetchEnabled), catalog.WithAudioURLLimiterClock(time.Now)),
		retryH:            retryH,
		reacquireH:        reacquireH,
		ytDlpSearcher:     audio.ytDlpSearcher,
		ytDlpAvailable:    audio.ytDlpAvailable,
		ytmCanary:         audio.ytmCanary,
		scheduler:         audio.background,
	}
}

func newOwnershipEnrichment(cat catalogWiring, bg *runloop.Tracker) *discovery.OwnershipEnrichmentService {
	return discovery.NewOwnershipEnrichmentService(
		bg,
		discovery.NewOwnershipReader(discoverycatalog.NewOwnedTracks(cat.trackRepo)),
		discoverycatalog.NewTrackNumberFiller(cat.setTrackNumberSvc),
	)
}
