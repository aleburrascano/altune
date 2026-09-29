package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
)

type SourceRegistry struct {
	sources []ports.AudioSource
}

func NewSourceRegistry(sources ...ports.AudioSource) *SourceRegistry {
	live := make([]ports.AudioSource, 0, len(sources))
	for _, s := range sources {
		if s != nil {
			live = append(live, s)
		}
	}
	return &SourceRegistry{sources: live}
}

func (r *SourceRegistry) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	candidates, _, err := r.FindReportingOutage(ctx, req)
	return candidates, err
}

func (r *SourceRegistry) FindReportingOutage(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error, error) {
	if len(r.sources) == 0 {
		return nil, nil, fmt.Errorf("no audio sources configured")
	}

	slots := make([][]ports.AudioCandidate, len(r.sources))
	errs := make([]error, len(r.sources))

	var wg sync.WaitGroup
	for i, source := range r.sources {
		wg.Add(1)
		go func(i int, source ports.AudioSource) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					errs[i] = fmt.Errorf("source %s panicked: %v", source.Name(), rec)
				}
			}()
			found, err := source.Find(ctx, req)
			if err != nil {
				errs[i] = fmt.Errorf("source %s: %w", source.Name(), err)
				return
			}
			slots[i] = stampSource(source.Name(), found)
		}(i, source)
	}
	wg.Wait()

	return mergeSlotsReportingOutage(ctx, r.sources, slots, errs)
}

func mergeSlots(
	ctx context.Context,
	sources []ports.AudioSource,
	slots [][]ports.AudioCandidate,
	errs []error,
) ([]ports.AudioCandidate, error) {
	merged, _, err := mergeSlotsReportingOutage(ctx, sources, slots, errs)
	return merged, err
}

func mergeSlotsReportingOutage(
	ctx context.Context,
	sources []ports.AudioSource,
	slots [][]ports.AudioCandidate,
	errs []error,
) ([]ports.AudioCandidate, error, error) {
	return ports.CollectCandidatesReportingOutage(
		len(sources), math.MaxInt,
		func(i int) ([]ports.AudioCandidate, error) { return slots[i], errs[i] },
		func(i int, candidates []ports.AudioCandidate) {
			slog.InfoContext(ctx, "acquisition.source_find_results",
				"source", sources[i].Name(), "candidates", len(candidates))
		},
		func(i int, err error) {
			slog.WarnContext(ctx, "acquisition.source_find_failed",
				"source", sources[i].Name(), "error", logSafeError(err))
		},
		func(firstErr error) error {
			return fmt.Errorf("every audio source failed: %w", firstErr)
		},
	)
}

func stampSource(name string, candidates []ports.AudioCandidate) []ports.AudioCandidate {
	out := make([]ports.AudioCandidate, 0, len(candidates))
	for _, c := range candidates {
		c.Source = name
		out = append(out, c)
	}
	return out
}

func (r *SourceRegistry) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error) {
	for _, s := range r.sources {
		if s.Name() == candidate.Source {
			return s.Fetch(ctx, candidate, outDir)
		}
	}
	return "", fmt.Errorf("no source named %q for candidate %q", candidate.Source, candidate.URL)
}

func (r *SourceRegistry) PreviewFetcherFor(candidate ports.AudioCandidate) (ports.PreviewFetcher, bool) {
	for _, s := range r.sources {
		if s.Name() == candidate.Source {
			fetcher, ok := s.(ports.PreviewFetcher)
			return fetcher, ok
		}
	}
	return nil, false
}
