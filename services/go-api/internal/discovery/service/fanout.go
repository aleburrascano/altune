package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

const defaultProviderTimeout = 1500 * time.Millisecond

func (s *Service) fanOut(
	ctx context.Context,
	searchQuery string,
	kinds map[domain.ResultKind]bool,
) ([][]domain.SearchResult, []domain.ProviderSearchResponse) {
	results := make([][]domain.SearchResult, len(s.providers))
	statuses := make([]domain.ProviderSearchResponse, len(s.providers))
	var wg sync.WaitGroup

	for i, provider := range s.providers {
		if !s.circuitBreaker.AllowRequest(provider.Name()) {
			statuses[i] = domain.ProviderSearchResponse{
				Provider: provider.Name(),
				Status:   domain.ProviderStatusCircuitOpen,
			}
			continue
		}

		wg.Add(1)
		go func(i int, p ports.SearchProvider) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					s.circuitBreaker.RecordFailure(p.Name())
					statuses[i] = domain.ProviderSearchResponse{
						Provider: p.Name(),
						Status:   domain.ProviderStatusError,
					}
					slog.ErrorContext(ctx, "search.v2.provider_panic",
						"provider", p.Name().String(), "panic", r)
				}
			}()

			timeout := defaultProviderTimeout
			if tp, ok := p.(interface{ SearchTimeout() time.Duration }); ok {
				timeout = tp.SearchTimeout()
			}
			provCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			start := time.Now()
			res, err := p.Search(provCtx, searchQuery, kinds)
			latencyMs := time.Since(start).Milliseconds()

			if err != nil {
				if ctx.Err() == nil {
					s.circuitBreaker.RecordFailure(p.Name())
				}
				status := domain.ProviderStatusError
				if provCtx.Err() != nil {
					status = domain.ProviderStatusTimeout
				}
				statuses[i] = domain.ProviderSearchResponse{
					Provider:  p.Name(),
					Status:    status,
					LatencyMs: latencyMs,
				}
				slog.WarnContext(ctx, "search.v2.provider_failed",
					"provider", p.Name().String(), "status", status.String(), "error", err)
				return
			}

			s.circuitBreaker.RecordSuccess(p.Name())
			results[i] = res
			statuses[i] = domain.ProviderSearchResponse{
				Provider:    p.Name(),
				Results:     res,
				Status:      domain.ProviderStatusOK,
				LatencyMs:   latencyMs,
				ResultCount: len(res),
			}
		}(i, provider)
	}

	wg.Wait()

	perProvider := make([][]domain.SearchResult, 0, len(s.providers))
	for _, r := range results {
		if len(r) > 0 {
			perProvider = append(perProvider, r)
		}
	}
	return perProvider, statuses
}

func anyProviderFailed(statuses []domain.ProviderSearchResponse) bool {
	for _, st := range statuses {
		if st.Status != domain.ProviderStatusOK {
			return true
		}
	}
	return false
}
