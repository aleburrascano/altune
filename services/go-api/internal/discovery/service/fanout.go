package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/redact"
)

const defaultProviderTimeout = 1500 * time.Millisecond

func (s *Service) fanOut(
	ctx context.Context,
	searchQuery string,
	kinds domain.ResultKindSet,
) ([][]domain.SearchResult, []domain.ProviderSearchResponse) {
	return s.fanOutVia(ctx, searchQuery, kinds, newProviderAnswers(s.providerCache, searchQuery, kinds))
}

func (s *Service) fanOutLive(
	ctx context.Context,
	searchQuery string,
	kinds domain.ResultKindSet,
) ([][]domain.SearchResult, []domain.ProviderSearchResponse) {
	return s.fanOutVia(ctx, searchQuery, kinds, providerAnswers{})
}

type providerArrival struct {
	slot    int
	results []domain.SearchResult
	status  domain.ProviderSearchResponse
}

type fanOutRun struct {
	results  [][]domain.SearchResult
	statuses []domain.ProviderSearchResponse
	waiting  map[int]bool
	arrivals chan providerArrival
	callCtx  context.Context
	stop     context.CancelFunc
	running  atomic.Int32
}

func newFanOutRun(ctx context.Context, providers int) *fanOutRun {
	callCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	if ctx.Err() != nil {
		stop()
	}
	run := &fanOutRun{
		results:  make([][]domain.SearchResult, providers),
		statuses: make([]domain.ProviderSearchResponse, providers),
		waiting:  make(map[int]bool, providers),
		arrivals: make(chan providerArrival, providers),
		callCtx:  callCtx,
		stop:     stop,
	}
	run.running.Store(1)
	return run
}

func (r *fanOutRun) callDone() {
	if r.running.Add(-1) == 0 {
		r.stop()
	}
}

func (r *fanOutRun) answered() [][]domain.SearchResult {
	perProvider := make([][]domain.SearchResult, 0, len(r.results))
	for _, res := range r.results {
		if len(res) > 0 {
			perProvider = append(perProvider, res)
		}
	}
	return perProvider
}

func (s *Service) fanOutVia(
	ctx context.Context,
	searchQuery string,
	kinds domain.ResultKindSet,
	answers providerAnswers,
) ([][]domain.SearchResult, []domain.ProviderSearchResponse) {
	run := newFanOutRun(ctx, len(s.providers))
	stopWithCaller := context.AfterFunc(ctx, run.stop)
	for i, provider := range s.providers {
		s.startProviderCall(ctx, run, i, provider, fanOutRequest{query: searchQuery, kinds: kinds, answers: answers})
	}
	run.callDone()

	s.collectArrivals(run)
	stopWithCaller()
	return run.answered(), run.statuses
}

type fanOutRequest struct {
	query   string
	kinds   domain.ResultKindSet
	answers providerAnswers
}

func (s *Service) startProviderCall(ctx context.Context, run *fanOutRun, slot int, provider ports.SearchProvider, req fanOutRequest) {
	if cached, hit := req.answers.get(ctx, provider.Name()); hit {
		run.results[slot], run.statuses[slot] = cached, cachedStatus(provider.Name(), cached)
		return
	}
	call, ok := admitProviderCall(ctx, s.circuitBreaker, provider.Name())
	if !ok {
		run.statuses[slot] = domain.ProviderSearchResponse{Provider: provider.Name(), Status: domain.ProviderStatusCircuitOpen}
		return
	}
	run.waiting[slot] = true
	run.running.Add(1)
	s.bg.track(func() { s.callProvider(run, slot, call, provider, req) })
}

func (s *Service) callProvider(run *fanOutRun, slot int, call breakerCall, provider ports.SearchProvider, req fanOutRequest) {
	defer run.callDone()
	res, status := s.searchProvider(run.callCtx, call, provider, req.query, req.kinds)
	if status.Status == domain.ProviderStatusOK {
		req.answers.set(run.callCtx, provider.Name(), res)
	}
	run.arrivals <- providerArrival{slot: slot, results: res, status: status}
}

func (s *Service) collectArrivals(run *fanOutRun) {
	deadline, stopTimer := s.deadlineTimer()
	defer stopTimer()
	pastDeadline := false

	for len(run.waiting) > 0 && (!pastDeadline || !anyAnswer(run.results, run.statuses)) {
		select {
		case a := <-run.arrivals:
			run.results[a.slot], run.statuses[a.slot] = a.results, a.status
			delete(run.waiting, a.slot)
		case <-deadline:
			pastDeadline, deadline = true, nil
		}
	}

	for slot := range run.waiting {
		run.statuses[slot] = domain.ProviderSearchResponse{Provider: s.providers[slot].Name(), Status: domain.ProviderStatusPending}
	}
}

func (s *Service) deadlineTimer() (<-chan time.Time, func() bool) {
	if s.deadline <= 0 {
		return nil, func() bool { return false }
	}
	timer := time.NewTimer(s.deadline)
	return timer.C, timer.Stop
}

func anyAnswer(results [][]domain.SearchResult, statuses []domain.ProviderSearchResponse) bool {
	for i, st := range statuses {
		if st.Status == domain.ProviderStatusOK && len(results[i]) > 0 {
			return true
		}
	}
	return false
}

func (s *Service) searchProvider(
	ctx context.Context,
	call breakerCall,
	p ports.SearchProvider,
	searchQuery string,
	kinds domain.ResultKindSet,
) (res []domain.SearchResult, status domain.ProviderSearchResponse) {
	defer func() {
		if r := recover(); r != nil {
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

	var latencyMs int64
	res, err := runAdmitted(ctx, call,
		func(err error) error { return searchBreakerOutcome(provCtx, err) },
		func() ([]domain.SearchResult, error) {
			start := time.Now()
			defer func() { latencyMs = time.Since(start).Milliseconds() }()
			return p.Search(provCtx, searchQuery, kinds)
		})

	if partial := (*domain.PartialKindsError)(nil); errors.As(err, &partial) {
		st := failureStatus(provCtx, err)
		slog.WarnContext(ctx, "search.v2.provider_partial",
			"provider", p.Name().String(), "status", st.String(), "failed_kinds", partial.Failed,
			"error", redact.Secrets(err.Error()))
		return res, domain.ProviderSearchResponse{
			Provider:    p.Name(),
			Results:     res,
			Status:      st,
			LatencyMs:   latencyMs,
			ResultCount: len(res),
		}
	}
	if err != nil {
		st := failureStatus(provCtx, err)
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

func failureStatus(provCtx context.Context, err error) domain.ProviderStatus {
	if provCtx.Err() != nil || errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) ||
		errors.Is(err, context.DeadlineExceeded) {
		return domain.ProviderStatusTimeout
	}
	return domain.ProviderStatusError
}

func searchBreakerOutcome(provCtx context.Context, err error) error {
	var partial *domain.PartialKindsError
	if errors.As(err, &partial) {
		return nil
	}
	return budgetOutcome(provCtx, err)
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
		if st.Status != domain.ProviderStatusOK && st.Status != domain.ProviderStatusPending {
			return true
		}
	}
	return false
}

func anyProviderPending(statuses []domain.ProviderSearchResponse) bool {
	for _, st := range statuses {
		if st.Status == domain.ProviderStatusPending {
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
