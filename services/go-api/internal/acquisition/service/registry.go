package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

var ErrNoAudioSources = errors.New("no audio sources configured")

type SourceRegistry struct {
	sources  []ports.AudioSource
	fallback []ports.AudioSource
}

func NewSourceRegistry(sources ...ports.AudioSource) *SourceRegistry {
	return &SourceRegistry{sources: liveSources(sources)}
}

func (r *SourceRegistry) WithFallback(sources ...ports.AudioSource) *SourceRegistry {
	r.fallback = liveSources(sources)
	return r
}

func liveSources(sources []ports.AudioSource) []ports.AudioSource {
	live := make([]ports.AudioSource, 0, len(sources))
	for _, s := range sources {
		if s != nil {
			live = append(live, s)
		}
	}
	return live
}

func (r *SourceRegistry) all() []ports.AudioSource {
	return append(append([]ports.AudioSource{}, r.sources...), r.fallback...)
}

func (r *SourceRegistry) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	res, err := r.FindReportingOutage(ctx, req)
	return res.Candidates, err
}

type FindResult struct {
	Candidates      []ports.AudioCandidate
	Outage          error
	FallbackSkipped bool
}

func (r *SourceRegistry) FindReportingOutage(ctx context.Context, req ports.FindRequest) (FindResult, error) {
	if len(r.sources) == 0 && len(r.fallback) == 0 {
		return FindResult{}, ErrNoAudioSources
	}

	if req.FallbackOnly {
		slots, errs := findAll(ctx, req, r.fallback)
		return mergeSlotsReportingOutage(ctx, r.fallback, slots, errs)
	}
	slots, errs := findAll(ctx, req, r.sources)
	if len(r.fallback) == 0 {
		return mergeSlotsReportingOutage(ctx, r.sources, slots, errs)
	}
	if anyResolved(slots) {
		res, err := mergeSlotsReportingOutage(ctx, r.sources, slots, errs)
		res.FallbackSkipped = true
		return res, err
	}
	fallbackSlots, fallbackErrs := findAll(ctx, req, r.fallback)
	return mergeSlotsReportingOutage(ctx, r.all(), append(slots, fallbackSlots...), append(errs, fallbackErrs...))
}

func findAll(ctx context.Context, req ports.FindRequest, sources []ports.AudioSource) ([][]ports.AudioCandidate, []error) {
	slots := make([][]ports.AudioCandidate, len(sources))
	errs := make([]error, len(sources))

	var wg sync.WaitGroup
	for i, source := range sources {
		wg.Add(1)
		go func(i int, source ports.AudioSource) {
			defer wg.Done()
			slots[i], errs[i] = findOne(ctx, req, source)
		}(i, source)
	}
	wg.Wait()
	return slots, errs
}

func findOne(ctx context.Context, req ports.FindRequest, source ports.AudioSource) (found []ports.AudioCandidate, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			found, err = nil, fmt.Errorf("source %s panicked: %v", source.Name(), rec)
		}
	}()
	candidates, err := source.Find(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("source %s: %w", source.Name(), err)
	}
	return stampSource(source.Name(), candidates), nil
}

func anyResolved(slots [][]ports.AudioCandidate) bool {
	for _, slot := range slots {
		for _, c := range slot {
			if c.IsResolved() {
				return true
			}
		}
	}
	return false
}

func mergeSlotsReportingOutage(
	ctx context.Context,
	sources []ports.AudioSource,
	slots [][]ports.AudioCandidate,
	errs []error,
) (FindResult, error) {
	candidates, outage, err := ports.CollectCandidatesReportingOutage(
		len(sources),
		func(i int) ([]ports.AudioCandidate, error) { return slots[i], errs[i] },
		func(i int, candidates []ports.AudioCandidate) {
			slog.InfoContext(ctx, "acquisition.source_find_results",
				"source", sources[i].Name(), "candidates", len(candidates))
		},
		func(i int, err error) {
			slog.WarnContext(ctx, "acquisition.source_find_failed",
				"source", sources[i].Name(), "error", redact.LogError(err))
		},
		func(firstErr error) error {
			return fmt.Errorf("every audio source failed: %w", firstErr)
		},
	)
	return FindResult{Candidates: candidates, Outage: outage}, err
}

func stampSource(name acqdomain.SourceName, candidates []ports.AudioCandidate) []ports.AudioCandidate {
	out := make([]ports.AudioCandidate, 0, len(candidates))
	for _, c := range candidates {
		c.Source = name
		out = append(out, c)
	}
	return out
}

func (r *SourceRegistry) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	for _, s := range r.all() {
		if s.Name() == candidate.Source {
			return s.Fetch(ctx, candidate, outDir)
		}
	}
	return ports.FetchedAudio{}, fmt.Errorf("no source named %q for candidate %q", candidate.Source, candidate.URL)
}

func (r *SourceRegistry) PreviewFetcherFor(candidate ports.AudioCandidate) (ports.PreviewFetcher, bool) {
	for _, s := range r.all() {
		if s.Name() == candidate.Source {
			fetcher, ok := s.(ports.PreviewFetcher)
			return fetcher, ok
		}
	}
	return nil, false
}
