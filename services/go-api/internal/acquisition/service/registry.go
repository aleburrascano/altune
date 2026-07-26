package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"altune/go-api/internal/acquisition/ports"
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

func (r *SourceRegistry) Names() []string {
	names := make([]string, 0, len(r.sources))
	for _, s := range r.sources {
		names = append(names, s.Name())
	}
	return names
}

func (r *SourceRegistry) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	if len(r.sources) == 0 {
		return nil, fmt.Errorf("no audio sources configured")
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

	return mergeSlots(ctx, r.sources, slots, errs)
}

func mergeSlots(
	ctx context.Context,
	sources []ports.AudioSource,
	slots [][]ports.AudioCandidate,
	errs []error,
) ([]ports.AudioCandidate, error) {
	seen := make(map[string]bool)
	var merged []ports.AudioCandidate
	var firstErr error
	failures := 0

	for i := range sources {
		if errs[i] != nil {
			failures++
			if firstErr == nil {
				firstErr = errs[i]
			}
			slog.WarnContext(ctx, "acquisition.source_find_failed",
				"source", sources[i].Name(), "error", errs[i])
			continue
		}
		slog.InfoContext(ctx, "acquisition.source_find_results",
			"source", sources[i].Name(), "candidates", len(slots[i]))
		merged = ports.DedupeCandidatesByURL(merged, slots[i], seen)
	}

	if failures == len(sources) {
		return nil, fmt.Errorf("every audio source failed: %w", firstErr)
	}
	return merged, nil
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
