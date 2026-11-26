package downloadfromsearch

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/service/query"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
)

type searcher interface {
	Execute(ctx context.Context, userId ids.UserId, query *domain.SearchQuery) (*query.SearchOutput, error)
}

type Search struct {
	searcher searcher
}

var _ download.RecordingSearch = Search{}

func NewSearch(s searcher) Search {
	return Search{searcher: s}
}

func (s Search) SearchRecordings(ctx context.Context, text string, limit int) ([]download.SearchedRecording, error) {
	tracksOnly, err := domain.NewSearchQuery(text, domain.ResultKindSet{domain.ResultKindTrack: true}, limit)
	if err != nil {
		return nil, fmt.Errorf("build resolve query: %w", err)
	}
	out, err := s.searcher.Execute(ctx, ids.UserId{}, tracksOnly)
	if err != nil || out == nil {
		return nil, err
	}
	return recordingsOf(out.Results), nil
}

func recordingsOf(results []domain.SearchResult) []download.SearchedRecording {
	var recordings []download.SearchedRecording
	for _, res := range results {
		if res.Kind != domain.ResultKindTrack {
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

func sourcesOf(refs []domain.ProviderLink) []download.ProviderRef {
	if len(refs) == 0 {
		return nil
	}
	sources := make([]download.ProviderRef, len(refs))
	for i, ref := range refs {
		sources[i] = download.ProviderRef{Provider: providerOf(ref.Provider), ExternalID: ref.ExternalID, URL: ref.URL}
	}
	return sources
}

func providerOf(p domain.ProviderName) download.RecordingProvider {
	switch p {
	case domain.ProviderYouTube:
		return download.ProviderYouTube
	case domain.ProviderDeezer:
		return download.ProviderDeezer
	case domain.ProviderSoundCloud:
		return download.ProviderSoundCloud
	default:
		return download.RecordingProvider(p.String())
	}
}
