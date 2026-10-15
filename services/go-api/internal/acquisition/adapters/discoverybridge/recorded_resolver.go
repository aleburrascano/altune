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
	ISRC     string
	MBID     string
	Duration int
}

type RecordedISRCRecording struct {
	MBID     string
	Duration int
}

func NewRecordedResolver(hit *RecordedSearchHit, isrcRecordings []RecordedISRCRecording, pinned *acqports.RecordingFacts) *RecordingResolver {
	return NewRecordingResolver(recordedSearch{hit: hit},
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
	hit *RecordedSearchHit
}

func (s recordedSearch) Execute(context.Context, shared.UserId, *discoverydomain.SearchQuery, bool) (*search.SearchOutput, error) {
	if s.hit == nil {
		return &search.SearchOutput{}, nil
	}
	return &search.SearchOutput{Results: []discoverydomain.SearchResult{{
		Kind:          discoverydomain.ResultKindTrack,
		Title:         s.hit.Title,
		Subtitle:      s.hit.Artist,
		ISRC:          s.hit.ISRC,
		RecordingMBID: s.hit.MBID,
		Duration:      s.hit.Duration,
	}}}, nil
}

type recordedISRCAuthority []RecordedISRCRecording

func (a recordedISRCAuthority) RecordingsByISRC(context.Context, string) ([]discoveryports.ISRCRecording, error) {
	recordings := make([]discoveryports.ISRCRecording, 0, len(a))
	for _, rec := range a {
		recordings = append(recordings, discoveryports.ISRCRecording{MBID: rec.MBID, Duration: rec.Duration})
	}
	return recordings, nil
}
