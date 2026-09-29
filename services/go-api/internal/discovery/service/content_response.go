package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"log/slog"
)

type ContentFetchResponse struct {
	ProviderName domain.ProviderName
	Status       domain.ProviderStatus
	Items        []domain.SearchResult
	Partial      bool
	Unserved     bool
	FallbackFrom domain.ProviderName
}

func failedContentResponse(providerName domain.ProviderName, status domain.ProviderStatus) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       status,
		Items:        []domain.SearchResult{},
	}
}

func unservedContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	resp := failedContentResponse(providerName, domain.ProviderStatusError)
	resp.Unserved = true
	return resp
}

func contentFailureStatus(err error) domain.ProviderStatus {
	var status httpStatusCoder
	if errors.As(err, &status) && status.HTTPStatus() == statusTooManyRequests {
		return domain.ProviderStatusRateLimited
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.ProviderStatusTimeout
	}
	var transport transportError
	if errors.As(err, &transport) && transport.Timeout() {
		return domain.ProviderStatusTimeout
	}
	return domain.ProviderStatusError
}

func fetchProviderResults(
	ctx context.Context,
	cb *CircuitBreaker,
	providerName domain.ProviderName,
	externalID, logKey string,
	fetch func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error),
) ([]domain.SearchResult, *ContentFetchResponse) {
	results, _, degraded := fetchProviderContent(ctx, cb, providerName, externalID, logKey, fetch)
	return results, degraded
}

func fetchProviderContent(
	ctx context.Context,
	cb *CircuitBreaker,
	providerName domain.ProviderName,
	externalID, logKey string,
	fetch func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error),
) (results []domain.SearchResult, partial bool, degraded *ContentFetchResponse) {
	results, err := guardedFetch(ctx, cb, providerName, func() ([]domain.SearchResult, error) {
		return fetch(ctx, providerName, externalID)
	})
	if errors.Is(err, errCircuitOpen) {
		return nil, false, circuitOpenContentResponse(providerName)
	}
	if isPartialResult(err) {
		return results, true, nil
	}
	if err != nil {
		status := contentFailureStatus(err)
		slog.WarnContext(ctx, logKey,
			"provider", providerName.String(), "external_id", externalID,
			"status", status.String(), "error", redact.Secrets(err.Error()))
		return nil, false, failedContentResponse(providerName, status)
	}
	return results, false, nil
}

func okContentResponse(providerName domain.ProviderName, results []domain.SearchResult, limit int) *ContentFetchResponse {
	if results == nil {
		results = []domain.SearchResult{}
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        results,
	}
}

func fallbackContentResponse(substitute, requested domain.ProviderName, results []domain.SearchResult, limit int) *ContentFetchResponse {
	resp := okContentResponse(substitute, results, limit)
	resp.FallbackFrom = requested
	return resp
}
