package download

import ports "altune/go-api/internal/download/internal/core/ports"

type (
	ISRCLookup        = ports.ISRCLookup
	ISRCRecording     = ports.ISRCRecording
	RecordingDetail   = ports.RecordingDetail
	RecordingLookup   = ports.RecordingLookup
	RecordingProvider = ports.RecordingProvider
	RecordingRelease  = ports.RecordingRelease
	RecordingSearch   = ports.RecordingSearch
	RerecordingLookup = ports.RerecordingLookup
	RerecordingQuery  = ports.RerecordingQuery
	SearchedRecording = ports.SearchedRecording
	WorkRecording     = ports.WorkRecording
)

const (
	ProviderDeezer     = ports.ProviderDeezer
	ProviderSoundCloud = ports.ProviderSoundCloud
)
