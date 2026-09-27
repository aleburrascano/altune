package discoverybridge

import (
	"altune/go-api/internal/shared"
	"context"

	discoverydomain "altune/go-api/internal/discovery/domain"
	discoveryports "altune/go-api/internal/discovery/ports"
	discoveryservice "altune/go-api/internal/discovery/service"
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

func NewRecordedResolver(hit *RecordedSearchHit, isrcRecordings []RecordedISRCRecording) *RecordingResolver {
	return NewRecordingResolver(recordedSearch{hit: hit}, WithISRCAuthority(recordedISRCAuthority(isrcRecordings)))
}

type recordedSearch struct {
	hit *RecordedSearchHit
}

func (s recordedSearch) Execute(context.Context, shared.UserId, *discoverydomain.SearchQuery, bool) (*discoveryservice.SearchOutput, error) {
	if s.hit == nil {
		return &discoveryservice.SearchOutput{}, nil
	}
	return &discoveryservice.SearchOutput{Results: []discoverydomain.SearchResult{{
		Kind:     discoverydomain.ResultKindTrack,
		Title:    s.hit.Title,
		Subtitle: s.hit.Artist,
		ISRC:     s.hit.ISRC,
		MBID:     s.hit.MBID,
		Duration: s.hit.Duration,
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
