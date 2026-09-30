package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/redact"
)

const defaultProviderTimeout = 1500 * time.Millisecond

var ErrAllProvidersFailed = errors.New("all providers failed")

func (s *Service) fanOut(
	ctx context.Context,
	searchQuery string,
	kinds map[domain.ResultKind]bool,
) ([][]domain.SearchResult, []domain.ProviderSearchResponse) {
	results := make([][]domain.SearchResult, len(s.providers))
	statuses := make([]domain.ProviderSearchResponse, len(s.providers))
	var wg sync.WaitGroup

	for i, provider := range s.providers {
		call, ok := admitProviderCall(s.circuitBreaker, provider.Name())
		if !ok {
			statuses[i] = domain.ProviderSearchResponse{
				Provider: provider.Name(),
				Status:   domain.ProviderStatusCircuitOpen,
			}
			continue
		}

		wg.Add(1)
		go func(i int, p ports.SearchProvider) {
			defer wg.Done()
			results[i], statuses[i] = s.searchProvider(ctx, call, p, searchQuery, kinds)
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

func (s *Service) searchProvider(
	ctx context.Context,
	call breakerCall,
	p ports.SearchProvider,
	searchQuery string,
	kinds map[domain.ResultKind]bool,
) (res []domain.SearchResult, status domain.ProviderSearchResponse) {
	settled := false
	defer func() {
		if r := recover(); r != nil {
			call.failPanicked(&settled)
			res = nil
			status = domain.ProviderSearchResponse{Provider: p.Name(), Status: domain.ProviderStatusError}
			slog.ErrorContext(ctx, "search.v2.provider_panic",
				"provider", p.Name().String(), "panic", r)
		}
	}()

	timeout := defaultProviderTimeout
	if tp, ok := p.(ports.SearchTimeouter); ok {
		timeout = tp.SearchTimeout()
	}
	provCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	res, err := p.Search(provCtx, searchQuery, kinds)
	latencyMs := time.Since(start).Milliseconds()
	settled = true
	call.settle(ctx, budgetOutcome(provCtx, err))

	if err != nil {
		st := domain.ProviderStatusError
		if provCtx.Err() != nil || errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
			st = domain.ProviderStatusTimeout
		}
		slog.WarnContext(ctx, "search.v2.provider_failed",
			"provider", p.Name().String(), "status", st.String(), "error", redact.Secrets(err.Error()))
		return nil, domain.ProviderSearchResponse{Provider: p.Name(), Status: st, LatencyMs: latencyMs}
	}
	return res, domain.ProviderSearchResponse{
		Provider:    p.Name(),
		Results:     res,
		Status:      domain.ProviderStatusOK,
		LatencyMs:   latencyMs,
		ResultCount: len(res),
	}
}

func budgetOutcome(provCtx context.Context, err error) error {
	if err == nil || !errors.Is(provCtx.Err(), context.DeadlineExceeded) ||
		errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: %w", err, context.DeadlineExceeded)
}

func anyProviderFailed(statuses []domain.ProviderSearchResponse) bool {
	for _, st := range statuses {
		if st.Status != domain.ProviderStatusOK {
			return true
		}
	}
	return false
}

func AllProvidersFailed(statuses []domain.ProviderSearchResponse) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, st := range statuses {
		if st.Status == domain.ProviderStatusOK {
			return false
		}
	}
	return true
}
