package acquisition

import ports "altune/go-api/internal/acquisition/internal/core/ports"

type (
	ISRCAuthority        = ports.ISRCAuthority
	ISRCRecording        = ports.ISRCRecording
	RecordingDetail      = ports.RecordingDetail
	RecordingLookup      = ports.RecordingLookup
	RecordingProvider    = ports.RecordingProvider
	RecordingRelease     = ports.RecordingRelease
	RecordingSearch      = ports.RecordingSearch
	RerecordingAuthority = ports.RerecordingAuthority
	RerecordingQuery     = ports.RerecordingQuery
	SearchedRecording    = ports.SearchedRecording
	WorkRecording        = ports.WorkRecording
)

const (
	ProviderDeezer     = ports.ProviderDeezer
	ProviderSoundCloud = ports.ProviderSoundCloud
)
