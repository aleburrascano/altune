package steps

import (
	"context"
	"fmt"

	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/ports"
)

type SearchStep struct {
	searcher ports.AudioSearcher
}

func NewSearchStep(searcher ports.AudioSearcher) *SearchStep {
	return &SearchStep{searcher: searcher}
}

func (s *SearchStep) Name() string { return "search" }

func (s *SearchStep) Execute(ctx context.Context, ac *service.AcquisitionContext) error {
	queries := buildSearchQueries(ac.Track)

	seen := make(map[string]bool)
	var allCandidates []service.Candidate

	for _, query := range queries {
		results, err := s.searcher.Search(ctx, query)
		if err != nil {
			continue
		}
		for _, r := range results {
			if seen[r.URL] {
				continue
			}
			seen[r.URL] = true
			allCandidates = append(allCandidates, service.Candidate{
				Title:         r.Title,
				Artist:        r.Artist,
				Duration:      r.DurationSecs,
				URL:           r.URL,
				Channel:       r.Channel,
				Categories:    r.Categories,
				ViewCount:     r.ViewCount,
				FollowerCount: r.FollowerCount,
			})
		}
	}

	if len(allCandidates) == 0 {
		return fmt.Errorf("no candidates found")
	}

	ac.Candidates = allCandidates
	return nil
}

func (s *SearchStep) Rollback(_ context.Context, _ *service.AcquisitionContext) error {
	return nil
}

func buildSearchQueries(track service.TrackRef) []string {
	var queries []string

	if track.ISRC != "" {
		queries = append(queries, track.ISRC)
	}

	queries = append(queries, track.Title+" "+track.Artist)

	if track.Album != "" {
		queries = append(queries, track.Title+" "+track.Artist+" "+track.Album)
	}

	queries = append(queries, track.Title+" "+track.Artist+" audio")

	return queries
}
