package library

import audio "altune/go-api/internal/library/internal/core/audio"

type (
	AudioLinkService   = audio.AudioLinkService
	StreamTrackService = audio.StreamTrackService
)

var (
	NewAudioURLService               = audio.NewAudioURLService
	NewReconcileOrphanedAudioService = audio.NewReconcileOrphanedAudioService
	NewStreamTrackService            = audio.NewStreamTrackService
	WithAudioURLClock                = audio.WithAudioURLClock
	WithAudioURLMetrics              = audio.WithAudioURLMetrics
	WithReconcileMetrics             = audio.WithReconcileMetrics
	WithReconcileSwitch              = audio.WithReconcileSwitch
	WithStreamMetrics                = audio.WithStreamMetrics
	WithStreamRecoverySwitch         = audio.WithStreamRecoverySwitch
	WithStreamScheduler              = audio.WithStreamScheduler
)
