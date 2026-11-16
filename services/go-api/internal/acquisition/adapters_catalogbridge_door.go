package acquisition

import catalogbridge "altune/go-api/internal/acquisition/internal/adapters/catalogbridge"

type TrackLedger = catalogbridge.TrackLedger

var (
	NewOrphanedAudioRecorder = catalogbridge.NewOrphanedAudioRecorder
	NewTrackLedger           = catalogbridge.NewTrackLedger
)
