package library

import (
	"altune/go-api/internal/library/internal/adapters/handler"
	"altune/go-api/internal/library/internal/adapters/persistence"
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/service/track"
	"altune/go-api/internal/shared/events"
	"errors"
)

var errDepsIncomplete = errors.New("library: New needs its three repositories, an events publisher, an acquisition scheduler, audio links (NoAudioLinks when the store cannot sign), audio metrics and a stream recovery switch")

type Deps struct {
	CatalogTracks   *persistence.PostgresCatalogTrackRepository
	Playlists       *persistence.PostgresPlaylistRepository
	OrphanedAudio   *persistence.PostgresOrphanedAudioRepository
	Events          events.Publisher
	Scheduler       ports.AcquisitionScheduler
	AudioStore      ports.AudioStore
	AudioLinks      ports.AudioLinkSigner
	AudioMetrics    ports.AudioStoreMetrics
	Featured        ports.FeaturedArtistResolver
	StreamRecovery  func() bool
	PrefetchEnabled bool
}

func (d Deps) validate() error {
	if d.CatalogTracks == nil || d.Playlists == nil || d.OrphanedAudio == nil || d.Events == nil || d.Scheduler == nil || d.AudioLinks == nil || d.AudioMetrics == nil || d.StreamRecovery == nil {
		return errDepsIncomplete
	}
	return nil
}

type Module struct {
	TrackHandler    *handler.TrackHandler
	LibraryHandler  *handler.LibraryHandler
	PlaylistHandler *handler.PlaylistHandler
	StreamHandler   *handler.StreamHandler
	AudioURLHandler *handler.AudioURLHandler
	SetTrackNumber  *track.SetTrackNumberService
	OrphanedAudio   *persistence.PostgresOrphanedAudioRepository
}
