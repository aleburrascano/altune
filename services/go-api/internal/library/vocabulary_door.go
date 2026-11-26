package library

import (
	domain "altune/go-api/internal/library/domain"
	audio "altune/go-api/internal/library/internal/core/audio"
	ports "altune/go-api/internal/library/internal/core/ports"
)

type (
	ArtistGroup               = domain.ArtistGroup
	AudioUsage                = ports.AudioUsage
	FeaturedArtistRepository  = ports.FeaturedArtistRepository
	LibraryViewRepository     = ports.LibraryViewRepository
	OrphanedAudioSweep        = audio.OrphanedAudioSweep
	CleanUpUnusedAudioService = audio.CleanUpUnusedAudioService
)

const (
	AudioOwnerAcquiring = ports.AudioOwnerAcquiring
	AudioReferenced     = ports.AudioReferenced
	AudioUnused         = ports.AudioUnused
)

var (
	ErrAudioOrphaned                 = domain.ErrAudioOrphaned
	ErrCatalogTemporarilyUnavailable = domain.ErrCatalogTemporarilyUnavailable
	ErrOrphanedAudioQueueUnavailable = ports.ErrOrphanedAudioQueueUnavailable
	ErrPlaylistNotFound              = domain.ErrPlaylistNotFound
	ErrTrackNotFound                 = domain.ErrTrackNotFound
	NewFeaturedArtistIdentityOnly    = domain.NewFeaturedArtistIdentityOnly
	NewPlaylistId                    = domain.NewPlaylistId
)
