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

func searchOnlyRecording(none RecordingWiring, searcher Search) (RecordingWiring, error) {
	resolver, err := download.NewRecordingResolver(download.RecordingResolverPorts{
		Search:      searcher,
		ISRC:        download.NoopISRCLookup(),
		Recording:   download.NoopRecordingLookup(),
		Rerecording: download.NoopRerecordingLookup(),
	})
	if err != nil {
		return RecordingWiring{}, err
	}
	none.Resolver = resolver
	return none, nil
}

func RecordingOptions(disc *search.Discovery) (RecordingWiring, error) {
	none := NoRecording()
	if disc.Search == nil {
		return none, nil
	}
	searcher := NewSearch(disc.Search)
	if disc.Recordings == nil {
		return searchOnlyRecording(none, searcher)
	}
	recordings := NewRecordings(disc.Recordings)
	resolver, err := download.NewRecordingResolver(download.RecordingResolverPorts{
		Search:      searcher,
		ISRC:        recordings,
		Recording:   recordings,
		Rerecording: recordings,
	})
	if err != nil {
		return RecordingWiring{}, err
	}
	return RecordingWiring{
		Resolver:     resolver,
		Lookup:       resolver,
		Rerecordings: resolver,
	}, nil
}
