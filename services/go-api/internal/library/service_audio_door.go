package library

import audio "altune/go-api/internal/library/internal/service/audio"

var (
	AlwaysEnabled                    = audio.AlwaysEnabled
	ErrAudioClockRequired            = audio.ErrClockRequired
	MustNewAudioURLService           = audio.MustNewAudioURLService
	NewAudioURLService               = audio.NewAudioURLService
	NewReconcileOrphanedAudioService = audio.NewReconcileOrphanedAudioService
	NewStreamTrackService            = audio.NewStreamTrackService
)
