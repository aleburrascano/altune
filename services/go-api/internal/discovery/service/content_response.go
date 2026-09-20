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
	// Partial mirrors SearchOutput.Partial: the answer was assembled while at
	// least one fanned-out provider failed, so Items may be incomplete even
	// though Status is ok.
	Partial bool
	// Unserved is true when no provider is wired for this content kind, so no
	// provider was called and Status says nothing about any provider's health.
	Unserved bool
	// FallbackFrom names the provider that was asked but had no answer, when a
	// substitute provider produced Items in its place. ProviderUnknown means
	// ProviderName answered for itself.
	FallbackFrom domain.ProviderName
}

// failedContentResponse is the degraded answer for a fetch that failed with
// status, one of the typed provider statuses search reports.
func failedContentResponse(providerName domain.ProviderName, status domain.ProviderStatus) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       status,
		Items:        []domain.SearchResult{},
	}
}

// unservedContentResponse is the error answer for a provider with no adapter
// wired for the requested content kind.
func unservedContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	resp := failedContentResponse(providerName, domain.ProviderStatusError)
	resp.Unserved = true
	return resp
}

// contentFailureStatus classifies a failed provider call into the provider
// status model search uses, so a caller can tell a slow upstream (retry now)
// from a throttled one from a plain failure. Any error that is neither a
// timeout nor an upstream 429 is ProviderStatusError.
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

// fetchProviderResults runs one single-provider content fetch through the
// circuit breaker (nil means ungated), turning a short-circuit or failure into
// the degraded response the caller returns as-is.
func fetchProviderResults(
	ctx context.Context,
	cb *CircuitBreaker,
	providerName domain.ProviderName,
	externalID, logKey string,
	fetch func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error),
) ([]domain.SearchResult, *ContentFetchResponse) {
	results, err := guardedFetch(ctx, cb, providerName, func() ([]domain.SearchResult, error) {
		return fetch(ctx, providerName, externalID)
	})
	if errors.Is(err, errCircuitOpen) {
		return nil, circuitOpenContentResponse(providerName)
	}
	if err != nil {
		status := contentFailureStatus(err)
		// A transport failure's *url.Error embeds the request URL, which for
		// LastFM and SoundCloud carries api_key / client_id.
		slog.WarnContext(ctx, logKey,
			"provider", providerName.String(), "external_id", externalID,
			"status", status.String(), "error", redact.Secrets(err.Error()))
		return nil, failedContentResponse(providerName, status)
	}
	return results, nil
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

// fallbackContentResponse is the answer substitute produced after requested had
// none, marked so a caller can tell a stand-in's content from the content the
// provider it asked for would have served.
func fallbackContentResponse(substitute, requested domain.ProviderName, results []domain.SearchResult, limit int) *ContentFetchResponse {
	resp := okContentResponse(substitute, results, limit)
	resp.FallbackFrom = requested
	return resp
}
