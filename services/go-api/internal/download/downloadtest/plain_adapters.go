package downloadtest

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"context"
	"fmt"
)

type Finder interface {
	Find(ctx context.Context, req domain.FindRequest) ([]domain.AudioCandidate, error)
}

type OutageFinder interface {
	FindReportingOutage(ctx context.Context, req domain.FindRequest) (service.FindResult, error)
}

type Fetcher interface {
	Fetch(ctx context.Context, candidate domain.AudioCandidate, outDir string) (domain.FetchedAudio, error)
}

type PreviewingFetcher interface {
	Fetcher
	PreviewFetcherFor(candidate domain.AudioCandidate) (ports.PreviewFetcher, bool)
}

type plainFinder struct{ finder Finder }

func PlainFinder(finder Finder) OutageFinder { return plainFinder{finder: finder} }

func (p plainFinder) FindReportingOutage(ctx context.Context, req domain.FindRequest) (service.FindResult, error) {
	candidates, err := p.finder.Find(ctx, req)
	if err != nil {
		return service.FindResult{Candidates: candidates}, fmt.Errorf("find candidates: %w", err)
	}
	return service.FindResult{Candidates: candidates}, nil
}

type plainFetcher struct{ fetcher Fetcher }

func PlainFetcher(fetcher Fetcher) PreviewingFetcher { return plainFetcher{fetcher: fetcher} }

func (p plainFetcher) Fetch(ctx context.Context, candidate domain.AudioCandidate, outDir string) (domain.FetchedAudio, error) {
	fetched, err := p.fetcher.Fetch(ctx, candidate, outDir)
	if err != nil {
		return fetched, fmt.Errorf("fetch the candidate: %w", err)
	}
	return fetched, nil
}

func (plainFetcher) PreviewFetcherFor(domain.AudioCandidate) (ports.PreviewFetcher, bool) {
	return nil, false
}
