package acquisitiondiscovery

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/discovery"
)

func RecordingOptions(disc *discovery.Discovery) []func(*acquisition.AcquireTrackAudioService) {
	if disc.Search == nil {
		return nil
	}
	search := NewSearch(disc.Search)
	if disc.Recordings == nil {
		return []func(*acquisition.AcquireTrackAudioService){acquisition.WithRecordingResolver(acquisition.NewRecordingResolver(search))}
	}
	recordings := NewRecordings(disc.Recordings)
	resolver := acquisition.NewRecordingResolver(search,
		acquisition.WithISRCAuthority(recordings),
		acquisition.WithRerecordingAuthority(recordings),
		acquisition.WithRecordingLookup(recordings))
	return []func(*acquisition.AcquireTrackAudioService){
		acquisition.WithRecordingResolver(resolver),
		acquisition.WithAcquireRecordingLookup(resolver),
		acquisition.WithRerecordingFinder(resolver),
	}
}
