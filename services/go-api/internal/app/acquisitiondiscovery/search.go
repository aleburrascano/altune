package acquisitiondiscovery

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/search"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"fmt"
)

type searcher interface {
	Execute(ctx context.Context, userId shared.UserId, query *domain.SearchQuery, saveHistory bool) (*search.SearchOutput, error)
}

type Search struct {
	searcher searcher
}

var _ acquisition.RecordingSearch = Search{}

func NewSearch(s searcher) Search {
	return Search{searcher: s}
}

func (s Search) SearchRecordings(ctx context.Context, query string, limit int) ([]acquisition.SearchedRecording, error) {
	tracksOnly, err := domain.NewSearchQuery(query, domain.ResultKindSet{domain.ResultKindTrack: true}, limit)
	if err != nil {
		return nil, fmt.Errorf("build resolve query: %w", err)
	}
	out, err := s.searcher.Execute(ctx, shared.UserId{}, tracksOnly, false)
	if err != nil || out == nil {
		return nil, err
	}
	return recordingsOf(out.Results), nil
}

func recordingsOf(results []domain.SearchResult) []acquisition.SearchedRecording {
	var recordings []acquisition.SearchedRecording
	for _, res := range results {
		if res.Kind != domain.ResultKindTrack {
			continue
		}
		recordings = append(recordings, acquisition.SearchedRecording{
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

func sourcesOf(refs []domain.SourceRef) []acquisition.ProviderRef {
	if len(refs) == 0 {
		return nil
	}
	sources := make([]acquisition.ProviderRef, len(refs))
	for i, ref := range refs {
		sources[i] = acquisition.ProviderRef{Provider: providerOf(ref.Provider), ExternalID: ref.ExternalID, URL: ref.URL}
	}
	return sources
}

func providerOf(p domain.ProviderName) acquisition.RecordingProvider {
	switch p {
	case domain.ProviderYouTube:
		return acquisition.ProviderYouTube
	case domain.ProviderDeezer:
		return acquisition.ProviderDeezer
	case domain.ProviderSoundCloud:
		return acquisition.ProviderSoundCloud
	default:
		return acquisition.RecordingProvider(p.String())
	}
}
