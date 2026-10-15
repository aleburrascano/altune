package discoverybridge

import (
	"altune/go-api/internal/discovery/service/search"
	"altune/go-api/internal/shared"
	"context"

	acqports "altune/go-api/internal/acquisition/ports"
	discoverydomain "altune/go-api/internal/discovery/domain"
	discoveryports "altune/go-api/internal/discovery/ports"
)

type RecordedSearchHit struct {
	Title    string
	Artist   string
	Album    string
	ISRC     string
	MBID     string
	Duration int
}

type RecordedISRCRecording struct {
	MBID     string
	Duration int
}

func NewRecordedResolver(hits []RecordedSearchHit, isrcRecordings []RecordedISRCRecording, pinned *acqports.RecordingFacts) *RecordingResolver {
	return NewRecordingResolver(recordedSearch{hits: hits},
		WithISRCAuthority(recordedISRCAuthority(isrcRecordings)),
		WithRecordingLookup(recordedLookup{pinned: pinned}))
}

type recordedLookup struct {
	pinned *acqports.RecordingFacts
}

func (l recordedLookup) RecordingByMBID(_ context.Context, mbid string) (acqports.RecordingFacts, bool, error) {
	if l.pinned == nil || l.pinned.MBID != mbid {
		return acqports.RecordingFacts{}, false, nil
	}
	return *l.pinned, true, nil
}

type recordedSearch struct {
	hits []RecordedSearchHit
}

func (s recordedSearch) Execute(context.Context, shared.UserId, *discoverydomain.SearchQuery, bool) (*search.SearchOutput, error) {
	results := make([]discoverydomain.SearchResult, 0, len(s.hits))
	for _, hit := range s.hits {
		results = append(results, discoverydomain.SearchResult{
			Kind:          discoverydomain.ResultKindTrack,
			Title:         hit.Title,
			Subtitle:      hit.Artist,
			Album:         hit.Album,
			ISRC:          hit.ISRC,
			RecordingMBID: hit.MBID,
			Duration:      hit.Duration,
		})
	}
	return &search.SearchOutput{Results: results}, nil
}

type recordedISRCAuthority []RecordedISRCRecording

func (a recordedISRCAuthority) RecordingsByISRC(context.Context, string) ([]discoveryports.ISRCRecording, error) {
	recordings := make([]discoveryports.ISRCRecording, 0, len(a))
	for _, rec := range a {
		recordings = append(recordings, discoveryports.ISRCRecording{MBID: rec.MBID, Duration: rec.Duration})
	}
	return recordings, nil
}
