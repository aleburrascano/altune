package downloadfromsearch

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/ids"
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

func (s Search) SearchRecordings(ctx context.Context, text string, limit int) ([]download.SearchedRecording, error) {
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

func recordingsOf(results []search.SearchResult) []download.SearchedRecording {
	var recordings []download.SearchedRecording
	for _, res := range results {
		if res.Kind != search.ResultKindTrack {
			continue
		}
		recordings = append(recordings, download.SearchedRecording{
			Title:    res.Title,
			Artist:   res.Subtitle,
			ISRC:     res.ISRC,
			MBID:     res.RecordingMBID,
			Duration: float64(res.Duration),
			Sources:  sourcesOf(res.Sources),
		})
	}
	return recordings
}

func sourcesOf(refs []search.ProviderLink) []download.ProviderRef {
	if len(refs) == 0 {
		return nil
	}
	sources := make([]download.ProviderRef, len(refs))
	for i, ref := range refs {
		sources[i] = download.ProviderRef{Provider: providerOf(ref.Provider), ExternalID: ref.ExternalID, URL: ref.URL}
	}
	return sources
}

func providerOf(p search.ProviderName) download.RecordingProvider {
	switch p {
	case search.ProviderYouTube:
		return download.ProviderYouTube
	case search.ProviderDeezer:
		return download.ProviderDeezer
	case search.ProviderSoundCloud:
		return download.ProviderSoundCloud
	default:
		return download.RecordingProvider(p.String())
	}
}
