package downloadfromsearch

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/search"
)

type RecordingWiring struct {
	Resolver     download.RecordingResolverPort
	Lookup       download.RecordingLookup
	Rerecordings download.RerecordingFinder
}

func NoRecording() RecordingWiring {
	return RecordingWiring{
		Resolver:     download.NoopRecordingResolver(),
		Lookup:       download.NoopRecordingLookup(),
		Rerecordings: download.NoopRerecordingFinder(),
	}
}

func RecordingOptions(disc *search.Discovery) RecordingWiring {
	none := NoRecording()
	if disc.Search == nil {
		return none
	}
	searcher := NewSearch(disc.Search)
	if disc.Recordings == nil {
		none.Resolver = download.MustNewRecordingResolver(download.RecordingResolverPorts{
			Search:      searcher,
			ISRC:        download.NoopISRCLookup(),
			Recording:   download.NoopRecordingLookup(),
			Rerecording: download.NoopRerecordingLookup(),
		})
		return none
	}
	recordings := NewRecordings(disc.Recordings)
	resolver := download.MustNewRecordingResolver(download.RecordingResolverPorts{
		Search:      searcher,
		ISRC:        recordings,
		Recording:   recordings,
		Rerecording: recordings,
	})
	return RecordingWiring{
		Resolver:     resolver,
		Lookup:       resolver,
		Rerecordings: resolver,
	}
}
