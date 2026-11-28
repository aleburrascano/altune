package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/parallel"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

var ErrNoAudioSources = errors.New("no audio sources configured")

type SourceRegistry struct {
	sources   []ports.AudioSource
	fallback  []ports.AudioSource
	previewed map[domain.SourceName]ports.PreviewFetcher
}

func NewSourceRegistry(sources ...ports.AudioSource) *SourceRegistry {
	return &SourceRegistry{sources: liveSources(sources)}
}

func NewSourceRegistryWithFallback(first, fallback []ports.AudioSource, previewing ...ports.PreviewingSource) *SourceRegistry {
	previewed := make(map[domain.SourceName]ports.PreviewFetcher, len(previewing))
	for _, source := range previewing {
		previewed[source.Name()] = source
	}
	return &SourceRegistry{sources: liveSources(first), fallback: liveSources(fallback), previewed: previewed}
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

func (r *SourceRegistry) Find(ctx context.Context, req domain.FindRequest) ([]domain.AudioCandidate, error) {
	res, err := r.FindReportingOutage(ctx, req)
	return res.Candidates, err
}

type FindResult struct {
	Candidates      []domain.AudioCandidate
	Outage          error
	FallbackSkipped bool
}

func (r *SourceRegistry) FindReportingOutage(ctx context.Context, req domain.FindRequest) (FindResult, error) {
	if len(r.sources) == 0 && len(r.fallback) == 0 {
		return FindResult{}, ErrNoAudioSources
	}

	if req.Tier == domain.TierFallbackOnly {
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

func findAll(ctx context.Context, req domain.FindRequest, sources []ports.AudioSource) ([][]domain.AudioCandidate, []error) {
	slots := make([][]domain.AudioCandidate, len(sources))
	errs := make([]error, len(sources))

	g := parallel.New("acquisition.source_find_panic", len(sources))
	for i, source := range sources {
		g.Go(ctx, func() { slots[i], errs[i] = findOne(ctx, req, source) })
	}
	_ = g.Wait()
	return slots, errs
}

func findOne(ctx context.Context, req domain.FindRequest, source ports.AudioSource) ([]domain.AudioCandidate, error) {
	var found []domain.AudioCandidate
	var err error
	panicked := background.SafeRun(ctx, "acquisition.source_find_panic", func() { found, err = findStamped(ctx, req, source) })
	if panicked != nil {
		return nil, fmt.Errorf("source %s: %w", source.Name(), panicked)
	}
	return found, err
}

func findStamped(ctx context.Context, req domain.FindRequest, source ports.AudioSource) ([]domain.AudioCandidate, error) {
	candidates, err := source.Find(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("source %s: %w", source.Name(), err)
	}
	return stampSource(source.Name(), candidates), nil
}

func anyResolved(slots [][]domain.AudioCandidate) bool {
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
	slots [][]domain.AudioCandidate,
	errs []error,
) (FindResult, error) {
	candidates, outage, err := domain.CollectCandidatesReportingOutage(
		len(sources),
		func(i int) ([]domain.AudioCandidate, error) { return slots[i], errs[i] },
		func(i int, candidates []domain.AudioCandidate) {
			slog.InfoContext(ctx, "acquisition.source_find_results",
				"source", sources[i].Name(), "candidates", len(candidates))
			for _, c := range candidates {
				if !c.HasSourceKey() {
					slog.WarnContext(ctx, "acquisition.candidate_dropped_no_source_key", "url", c.URL, "source", c.Source)
				}
			}
		},
		func(i int, err error) {
			slog.WarnContext(ctx, "acquisition.source_find_failed",
				"source", sources[i].Name(), logging.KeyError, redact.LogError(err))
		},
		func(firstErr error) error {
			return fmt.Errorf("every audio source failed: %w", firstErr)
		},
	)
	if err != nil {
		return FindResult{Candidates: candidates, Outage: outage}, fmt.Errorf("find candidates across sources: %w", err)
	}
	return FindResult{Candidates: candidates, Outage: outage}, nil
}

func stampSource(name domain.SourceName, candidates []domain.AudioCandidate) []domain.AudioCandidate {
	out := make([]domain.AudioCandidate, 0, len(candidates))
	for _, c := range candidates {
		c.Source = name
		out = append(out, c)
	}
	return out
}

func (r *SourceRegistry) Fetch(ctx context.Context, candidate domain.AudioCandidate, outDir string) (domain.FetchedAudio, error) {
	for _, s := range r.all() {
		if s.Name() == candidate.Source {
			return s.Fetch(ctx, candidate, outDir)
		}
	}
	return domain.FetchedAudio{}, fmt.Errorf("no source named %q for candidate %q", candidate.Source, candidate.URL)
}

func (r *SourceRegistry) PreviewFetcherFor(candidate domain.AudioCandidate) (ports.PreviewFetcher, bool) {
	fetcher, ok := r.previewed[candidate.Source]
	return fetcher, ok
}
