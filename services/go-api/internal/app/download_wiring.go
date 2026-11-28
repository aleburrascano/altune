package app

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"net/http"
	"time"
)

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
	sources, err := download.LiveSources(ctx, a.cfg, a.redisClient, transport, newOutboundClient)
	if err != nil {
		return nil, fmt.Errorf("wire audio sources: %w", err)
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
		Sources:           sources,
	})
	if err != nil {
		return nil, fmt.Errorf("build download: %w", err)
	}
	return module, nil
}

func newOutboundClient(timeout time.Duration, transport http.RoundTripper) *http.Client {
	return outbound.MustNewClient(timeout, transport)
}
