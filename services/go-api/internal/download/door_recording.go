package download

import ports "altune/go-api/internal/download/internal/core/ports"

type (
	ISRCLookup            = ports.ISRCLookup
	ISRCRecording         = ports.ISRCRecording
	RecordingDetail       = ports.RecordingDetail
	RecordingLookup       = ports.RecordingLookup
	RecordingResolverPort = ports.RecordingResolver
	RecordingProvider     = ports.RecordingProvider
	RecordingRelease      = ports.RecordingRelease
	RecordingSearch       = ports.RecordingSearch
	RerecordingLookup     = ports.RerecordingLookup
	RerecordingFinder     = ports.RerecordingFinder
	WorkRecording         = ports.WorkRecording
)

const (
	ProviderDeezer     = ports.ProviderDeezer
	ProviderSoundCloud = ports.ProviderSoundCloud
)
