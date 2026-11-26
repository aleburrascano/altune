package library

import audio "altune/go-api/internal/library/internal/core/audio"

type (
	AudioLinkService   = audio.AudioLinkService
	StreamTrackService = audio.StreamTrackService
)

var (
	ErrAudioClockRequired            = audio.ErrClockRequired
	MustNewAudioURLService           = audio.MustNewAudioURLService
	NewAudioURLService               = audio.NewAudioURLService
	NewReconcileOrphanedAudioService = audio.NewReconcileOrphanedAudioService
	NewStreamTrackService            = audio.NewStreamTrackService
	WithAudioURLMetrics              = audio.WithAudioURLMetrics
	WithReconcileMetrics             = audio.WithReconcileMetrics
	WithReconcileSwitch              = audio.WithReconcileSwitch
	WithStreamMetrics                = audio.WithStreamMetrics
	WithStreamRecoverySwitch         = audio.WithStreamRecoverySwitch
	WithStreamScheduler              = audio.WithStreamScheduler
)
