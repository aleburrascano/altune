package catalog

import audio "altune/go-api/internal/catalog/service/audio"

type (
	AudioURLService    = audio.AudioURLService
	StreamTrackService = audio.StreamTrackService
)

var (
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
