package ytdlp

import (
	"context"
	"log/slog"

	"altune/go-api/internal/acquisition/ports"
)

const SourceName = "ytdlp"

var _ ports.AudioSource = (*Source)(nil)

type Source struct {
	searcher *YtDlpAudioSearcher
}

func NewSource(searcher *YtDlpAudioSearcher) *Source {
	return &Source{searcher: searcher}
}

func (s *Source) Name() string { return SourceName }

func (s *Source) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	seen := make(map[string]bool)
	var merged []ports.AudioCandidate
	var firstErr error
	failures := 0

	queries := ports.SearchQueries(req)
	for _, query := range queries {
		slog.InfoContext(ctx, "acquisition.search_query", "query", query)
		results, err := s.searcher.Search(ctx, query)
		if err != nil {
			failures++
			if firstErr == nil {
				firstErr = err
			}
			slog.WarnContext(ctx, "acquisition.search_query_failed", "query", query, "error", err)
			continue
		}
		slog.InfoContext(ctx, "acquisition.search_query_results", "query", query, "candidates", len(results))
		merged = ports.DedupeCandidatesByURL(merged, results, seen)
	}

	if failures == len(queries) && firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error) {
	return s.searcher.Download(ctx, candidate.URL, outDir)
}
