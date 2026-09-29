package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
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
	candidates, err := s.searcher.SearchQueries(ctx, ports.SearchQueries(req))
	if err != nil {
		return nil, err
	}
	s.searcher.MarkUnplayable(ctx, candidates)
	return candidates, nil
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error) {
	return s.searcher.Download(ctx, candidate.URL, outDir)
}
