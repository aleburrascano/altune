package downloadtest

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"context"
	"fmt"
)

type Finder interface {
	Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error)
}

type OutageFinder interface {
	FindReportingOutage(ctx context.Context, req ports.FindRequest) (service.FindResult, error)
}

type Fetcher interface {
	Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error)
}

type PreviewingFetcher interface {
	Fetcher
	PreviewFetcherFor(candidate ports.AudioCandidate) (ports.PreviewFetcher, bool)
}

type plainFinder struct{ finder Finder }

func PlainFinder(finder Finder) OutageFinder { return plainFinder{finder: finder} }

func (p plainFinder) FindReportingOutage(ctx context.Context, req ports.FindRequest) (service.FindResult, error) {
	candidates, err := p.finder.Find(ctx, req)
	if err != nil {
		return service.FindResult{Candidates: candidates}, fmt.Errorf("%w", err)
	}
	return service.FindResult{Candidates: candidates}, nil
}

type plainFetcher struct{ fetcher Fetcher }

func PlainFetcher(fetcher Fetcher) PreviewingFetcher { return plainFetcher{fetcher: fetcher} }

func (p plainFetcher) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	fetched, err := p.fetcher.Fetch(ctx, candidate, outDir)
	if err != nil {
		return fetched, fmt.Errorf("%w", err)
	}
	return fetched, nil
}

func (plainFetcher) PreviewFetcherFor(ports.AudioCandidate) (ports.PreviewFetcher, bool) {
	return nil, false
}
