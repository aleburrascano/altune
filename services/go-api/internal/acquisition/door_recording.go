package acquisition

import ports "altune/go-api/internal/acquisition/internal/core/ports"

type (
	RecordingLookup   = ports.RecordingLookup
	RecordingProvider = ports.RecordingProvider
	RerecordingQuery  = ports.RerecordingQuery
)

const (
	ProviderDeezer     = ports.ProviderDeezer
	ProviderSoundCloud = ports.ProviderSoundCloud
)
