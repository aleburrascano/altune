package librarytest

import (
	"altune/go-api/internal/library/internal/adapters/storage"
	"altune/go-api/internal/library/internal/adapters/telemetry"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/service"
)

type (
	NewTrackInput   = domain.NewTrackInput
	FeaturedArtist  = domain.FeaturedArtist
	MetricsSnapshot = telemetry.Snapshot
)

var (
	ErrAudioNotAvailable           = domain.ErrAudioNotAvailable
	ErrAudioTemporarilyUnavailable = domain.ErrAudioTemporarilyUnavailable
	ErrTrackMissing                = domain.ErrTrackMissing
	KnownCatalogProviders          = domain.KnownCatalogProviders
	NewTrack                       = domain.NewTrack
	NewStreamTrackService          = service.NewStreamTrackService
	NewFilesystemAudioStore        = storage.NewFilesystemAudioStore
)
