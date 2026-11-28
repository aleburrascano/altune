package app

import (
	"altune/go-api/internal/app/bridges/libraryfromsearch"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
		StreamRecovery:  a.jobs.Switch(jobStreamRecovery),
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
