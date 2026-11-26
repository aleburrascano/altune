package downloadfromsearch

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/search"
)

func RecordingOptions(disc *search.Discovery) []func(*download.AcquireTrackAudioService) {
	if disc.Search == nil {
		return nil
	}
	searcher := NewSearch(disc.Search)
	if disc.Recordings == nil {
		return []func(*download.AcquireTrackAudioService){download.WithRecordingResolver(download.NewRecordingResolver(searcher))}
	}
	recordings := NewRecordings(disc.Recordings)
	resolver := download.NewRecordingResolver(searcher,
		download.WithISRCLookup(recordings),
		download.WithRerecordingLookup(recordings),
		download.WithRecordingLookup(recordings))
	return []func(*download.AcquireTrackAudioService){
		download.WithRecordingResolver(resolver),
		download.WithAcquireRecordingLookup(resolver),
		download.WithRerecordingFinder(resolver),
	}
}
