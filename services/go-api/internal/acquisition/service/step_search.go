package service

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/acquisition/ports"
)

type SearchStep struct {
	searcher ports.AudioSearcher
}

func NewSearchStep(searcher ports.AudioSearcher) *SearchStep {
	return &SearchStep{searcher: searcher}
}

func (s *SearchStep) Name() string { return "search" }

func (s *SearchStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	queries := buildSearchQueries(ac.Track)

	seen := make(map[string]bool)
	var allCandidates []ports.AudioCandidate

	for _, query := range queries {
		slog.InfoContext(ctx, "acquisition.search_query", "query", query)
		results, err := s.searcher.Search(ctx, query)
		if err != nil {
			slog.WarnContext(ctx, "acquisition.search_query_failed", "query", query, "error", err)
			continue
		}
		slog.InfoContext(ctx, "acquisition.search_query_results", "query", query, "candidates", len(results))
		allCandidates = ports.DedupeCandidatesByURL(allCandidates, results, seen)
	}

	if len(allCandidates) == 0 {
		return fmt.Errorf("no candidates found")
	}

	ac.Candidates = allCandidates
	return nil
}

func (s *SearchStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}

func buildSearchQueries(track TrackRef) []string {
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
