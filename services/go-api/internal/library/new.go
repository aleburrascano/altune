package library

import (
	"altune/go-api/internal/library/internal/adapters/handler"
	"altune/go-api/internal/library/internal/core/libraryviews"
	audioservice "altune/go-api/internal/library/internal/service/audio"
	"altune/go-api/internal/library/internal/service/featured"
	"altune/go-api/internal/library/internal/service/playlist"
	"altune/go-api/internal/library/internal/service/track"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"fmt"
)

func New(d Deps) (*Module, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	catalogTracks, playlists, orphanedAudio := d.CatalogTracks, d.Playlists, d.OrphanedAudio

	addTrack, err := track.NewAddTrackService(catalogTracks, d.Events, d.Scheduler, clock.Now, ids.Real)
	if err != nil {
		return nil, fmt.Errorf("build the add-track service: %w", err)
	}
	playlistLifecycle, err := playlist.NewPlaylistLifecycleService(playlists, d.Events, clock.Now, ids.Real)
	if err != nil {
		return nil, fmt.Errorf("build the playlist lifecycle service: %w", err)
	}
	playlistMembership, err := playlist.NewPlaylistMembershipService(playlists, catalogTracks, d.Events, clock.Now)
	if err != nil {
		return nil, fmt.Errorf("build the playlist membership service: %w", err)
	}

	setTrackNumber := track.NewSetTrackNumberService(catalogTracks)
	listTracks := track.NewListTracksService(catalogTracks)
	trackStatus := track.NewGetTrackStatusService(catalogTracks)
	deleteTrack := track.NewDeleteTrackService(catalogTracks, d.AudioStore, d.Events, d.AudioMetrics, orphanedAudio)
	backfillFeatured := featured.MustNewBackfillFeaturedService(catalogTracks, catalogTracks, d.Featured, clock.Now)
	listFeaturing := featured.NewListFeaturingService(catalogTracks)
	streamTrack := audioservice.NewStreamTrackService(catalogTracks, d.AudioStore, d.Scheduler, d.AudioMetrics, d.StreamRecovery, clock.Now)
	audioLink := audioservice.MustNewAudioURLService(catalogTracks, d.AudioLinks, clock.Now, d.AudioMetrics)

	featuredHandler := handler.NewFeaturedArtistHandler(backfillFeatured, listFeaturing)
	return &Module{
		TrackHandler:    handler.MustNewTrackHandler(addTrack, listTracks, trackStatus, deleteTrack, setTrackNumber, featuredHandler, clock.Now),
		LibraryHandler:  handler.NewLibraryHandler(libraryviews.NewLibraryLensService(catalogTracks)),
		PlaylistHandler: handler.MustNewPlaylistHandler(playlistLifecycle, playlistMembership, clock.Now),
		StreamHandler:   handler.MustNewStreamHandler(streamTrack, clock.Now),
		AudioURLHandler: handler.MustNewAudioURLHandler(audioLink, clock.Now, handler.WithPrefetchEnabled(d.PrefetchEnabled)),
		SetTrackNumber:  setTrackNumber,
		OrphanedAudio:   orphanedAudio,
	}, nil
}
