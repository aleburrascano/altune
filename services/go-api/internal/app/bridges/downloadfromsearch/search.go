package downloadfromsearch

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/songfields"
	"context"
	"fmt"
)

type searcher interface {
	Execute(ctx context.Context, userId ids.UserId, query *search.SearchQuery) (*search.SearchOutput, error)
}

type Search struct {
	searcher searcher
}

var _ download.RecordingSearch = Search{}

func NewSearch(s searcher) Search {
	return Search{searcher: s}
}

func (s Search) SearchRecordings(ctx context.Context, text string, limit int) ([]songfields.RecordingIdentity, error) {
	tracksOnly, err := search.NewSearchQuery(text, search.ResultKindSet{search.ResultKindTrack: true}, limit)
	if err != nil {
		return nil, fmt.Errorf("build resolve query: %w", err)
	}
	out, err := s.searcher.Execute(ctx, ids.UserId{}, tracksOnly)
	if err != nil {
		return nil, fmt.Errorf("search recordings: %w", err)
	}
	if out == nil {
		return nil, nil
	}
	return recordingsOf(out.Results), nil
}

func recordingsOf(results []search.SearchResult) []songfields.RecordingIdentity {
	var recordings []songfields.RecordingIdentity
	for _, res := range results {
		if res.Kind != search.ResultKindTrack {
			continue
		}
		recordings = append(recordings, res.RecordingIdentity())
	}
	return recordings
}
