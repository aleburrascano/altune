package catalog

import (
	domain "altune/go-api/internal/catalog/domain"
	audio "altune/go-api/internal/catalog/internal/core/audio"
	ports "altune/go-api/internal/catalog/internal/core/ports"
)

type (
	ArtistGroup                   = domain.ArtistGroup
	AudioUsage                    = ports.AudioUsage
	FeaturedArtistRepository      = ports.FeaturedArtistRepository
	LibraryLensRepository         = ports.LibraryLensRepository
	OrphanedAudioSweep            = audio.OrphanedAudioSweep
	ReconcileOrphanedAudioService = audio.ReconcileOrphanedAudioService
)

const AudioReferenced = ports.AudioReferenced

var (
	ErrAudioOrphaned                 = domain.ErrAudioOrphaned
	ErrCatalogTemporarilyUnavailable = domain.ErrCatalogTemporarilyUnavailable
	ErrOrphanedAudioQueueUnavailable = ports.ErrOrphanedAudioQueueUnavailable
	ErrPlaylistNotFound              = domain.ErrPlaylistNotFound
	ErrTrackNotFound                 = domain.ErrTrackNotFound
	NewFeaturedArtistIdentityOnly    = domain.NewFeaturedArtistIdentityOnly
	NewPlaylistId                    = domain.NewPlaylistId
)
