package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
)

type ContentFetchResponse struct {
	ProviderName domain.ProviderName
	Status       domain.ProviderStatus
	Items        []domain.SearchResult
}

func errorContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusError,
		Items:        []domain.SearchResult{},
	}
}

func emptyContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        []domain.SearchResult{},
	}
}

func fetchProviderResults(
	ctx context.Context,
	providerName domain.ProviderName,
	externalID, logKey string,
	fetch func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error),
) ([]domain.SearchResult, *ContentFetchResponse) {
	results, err := fetch(ctx, providerName, externalID)
	if err != nil {
		slog.WarnContext(ctx, logKey,
			"provider", providerName.String(), "external_id", externalID, "error", err)
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
