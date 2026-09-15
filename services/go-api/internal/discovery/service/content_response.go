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
}

func errorContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusError,
		Items:        []domain.SearchResult{},
	}
}

// unservedContentResponse is the error answer for a provider with no adapter
// wired for the requested content kind.
func unservedContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	resp := errorContentResponse(providerName)
	resp.Unserved = true
	return resp
}

func emptyContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        []domain.SearchResult{},
	}
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
		// A transport failure's *url.Error embeds the request URL, which for
		// LastFM and SoundCloud carries api_key / client_id.
		slog.WarnContext(ctx, logKey,
			"provider", providerName.String(), "external_id", externalID, "error", redact.Secrets(err.Error()))
		return nil, errorContentResponse(providerName)
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
