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
	queries := ports.SearchQueries(req)
	return ports.CollectCandidates(
		len(queries),
		func(i int) ([]ports.AudioCandidate, error) {
			slog.InfoContext(ctx, "acquisition.search_query", "query", queries[i])
			return s.searcher.Search(ctx, queries[i])
		},
		func(i int, results []ports.AudioCandidate) {
			slog.InfoContext(ctx, "acquisition.search_query_results",
				"query", queries[i], "candidates", len(results))
		},
		func(i int, err error) {
			slog.WarnContext(ctx, "acquisition.search_query_failed",
				"query", queries[i], "error", err)
		},
		func(firstErr error) error { return firstErr },
	)
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error) {
	return s.searcher.Download(ctx, candidate.URL, outDir)
}
