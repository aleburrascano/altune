package app

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/app/bridges/libraryfromsearch"
	"altune/go-api/internal/app/bridges/searchfromlibrary"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
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
	libraryRoutes     library.Routes
	download          *download.Module
}

func (a *application) wireCatalog(
	ctx context.Context,
	transport http.RoundTripper,
	tap *monitoring.EventTap,
	disc *search.Module,
) (catalogWiring, error) {
	backend, err := library.NewAudioStoreFromConfig(a.cfg, slog.Default(), library.NewFSCallMetrics(a.metrics))
	if err != nil {
		return catalogWiring{}, fmt.Errorf("call failed: %w", err)
	}
	dbMetrics := library.NewDBCallMetrics(a.metrics)
	trackRepo := library.NewPostgresTrackRepository(a.pool, dbMetrics)
	downloads, err := a.wireDownload(ctx, transport, tap, disc, backend.AudioStore, trackRepo, dbMetrics)
	if err != nil {
		return catalogWiring{}, err
	}
	scheduler := library.NoopAcquisitionScheduler()
	if downloads.On() {
		scheduler = downloads.Scheduler()
	}
	module, err := a.wireLibrary(tap, disc, scheduler, backend.AudioStore, backend.Links, dbMetrics)
	if err != nil {
		return catalogWiring{}, err
	}
	return catalogWiring{
		trackRepo:         trackRepo,
		audioStore:        backend.AudioStore,
		orphanedAudio:     module.OrphanedAudio,
		setTrackNumberSvc: module.SetTrackNumber,
		libraryRoutes:     module.Routes(),
		download:          downloads,
	}, nil
}

func (a *application) wireLibrary(
	tap *monitoring.EventTap,
	disc *search.Module,
	scheduler library.AcquisitionScheduler,
	audioStore library.AudioStore,
	audioLinks library.AudioLinkSigner,
	dbMetrics library.DBCallMetrics,
) (*library.Module, error) {
	module, err := library.New(library.Deps{
		DB:              a.pool,
		DBMetrics:       dbMetrics,
		Events:          tap,
		Scheduler:       scheduler,
		AudioStore:      audioStore,
		AudioLinks:      audioLinks,
		AudioMetrics:    library.NewAudioStoreMetrics(a.metrics),
		Featured:        featuredBridge(disc),
		StreamRecovery:  a.jobs.Switch(jobs.StreamRecovery),
		PrefetchEnabled: a.cfg.AudioPrefetchEnabled,
	})
	if err != nil {
		return nil, fmt.Errorf("build the library: %w", err)
	}
	return module, nil
}

func featuredBridge(disc *search.Module) library.FeaturedArtistResolver {
	if disc.FeaturedArtists == nil {
		return nil
	}
	return library.NewFeaturedArtistResolver(libraryfromsearch.NewFeaturedArtistCredits(disc.FeaturedArtists))
}

func (a *application) wireDownload(
	ctx context.Context,
	transport http.RoundTripper,
	tap *monitoring.EventTap,
	disc *search.Module,
	audioStore library.AudioStore,
	trackRepo *library.PostgresTrackRepository,
	dbMetrics library.DBCallMetrics,
) (*download.Module, error) {
	recording, err := downloadfromsearch.RecordingOptions(disc)
	if err != nil {
		return nil, fmt.Errorf("wire recording resolver: %w", err)
	}
	module, err := download.New(ctx, download.Deps{
		Config:            a.cfg,
		Transport:         transport,
		Database:          download.NewDatabase(a.pool),
		Redis:             a.redisClient,
		Events:            tap,
		NewClient:         newOutboundClient,
		Library:           downloadfromlibrary.NewLibraryTracks(trackRepo, clock.Now, ids.Real),
		InPlaceVerdicts:   downloadfromlibrary.NewInPlaceVerdicts(trackRepo),
		AudioStore:        audioStore,
		StoredAudio:       downloadfromlibrary.NewStoredAudioReader(audioStore),
		Orphans:           downloadfromlibrary.NewOrphanedAudioRecorder(library.NewPostgresOrphanedAudioRepository(a.pool, dbMetrics)),
		RecordingResolver: recording.Resolver,
		RecordingLookup:   recording.Lookup,
		Rerecordings:      recording.Rerecordings,
	})
	if err != nil {
		return nil, fmt.Errorf("build download: %w", err)
	}
	return module, nil
}

func newOutboundClient(timeout time.Duration, transport http.RoundTripper) *http.Client {
	return outbound.MustNewClient(timeout, transport)
}

func newOwnershipEnrichment(cat catalogWiring, bg *background.Tracker) (*search.OwnershipEnrichmentService, error) {
	r0, err := search.NewOwnershipEnrichmentService(
		bg,
		search.NewOwnershipReader(searchfromlibrary.NewOwnedTracks(cat.trackRepo)),
		searchfromlibrary.NewTrackNumberFiller(cat.setTrackNumberSvc),
	)
	if err != nil {
		return r0, fmt.Errorf("call failed: %w", err)
	}
	return r0, nil
}
