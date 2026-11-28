package ytdlp

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"context"
)

const SourceName domain.SourceName = "ytdlp"

var (
	_ ports.AudioSource    = (*Source)(nil)
	_ ports.PreviewFetcher = (*Source)(nil)
)

type Source struct {
	searcher *YouTubeAudioSearcher
}

func NewSource(searcher *YouTubeAudioSearcher) *Source {
	return &Source{searcher: searcher}
}

func (s *Source) Name() domain.SourceName { return SourceName }

func (s *Source) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	candidates, err := s.searcher.SearchQueries(ctx, searchQueries(req))
	if err != nil {
		return nil, err
	}
	s.searcher.MarkUnplayable(ctx, candidates)
	return candidates, nil
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	return s.searcher.Download(ctx, candidate.URL, outDir)
}

func (s *Source) FetchPreview(ctx context.Context, candidate ports.AudioCandidate, outDir string, seconds int) (string, error) {
	return s.searcher.DownloadPreview(ctx, candidate.URL, outDir, seconds)
}
