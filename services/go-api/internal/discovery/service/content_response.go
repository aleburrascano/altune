package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
)

// ContentFetchResponse is the envelope returned by every content use case
// (album tracks, artist top-tracks, related tracks). A non-nil Items slice
// is always present so the wire serializes [] rather than null.
type ContentFetchResponse struct {
	ProviderName domain.ProviderName
	Status       domain.ProviderStatus
	Items        []domain.SearchResult
}

// errorContentResponse is the degraded envelope every content use case returns
// when a provider is missing or fails: an error status with a non-nil empty
// item slice (so the wire serializes [] rather than null).
func errorContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusError,
		Items:        []domain.SearchResult{},
	}
}

// emptyContentResponse is the OK-but-nothing-found envelope: a healthy fetch that
// produced no items, again with a non-nil empty slice for the wire.
func emptyContentResponse(providerName domain.ProviderName) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        []domain.SearchResult{},
	}
}

// fetchProviderResults runs the fetch → warn → degrade contract shared by the
// content use cases. The caller is responsible for the provider-not-found guard
// (if !ok { degraded = errorContentResponse(...) }); this function is only called
// when a real provider is available, so fetch never captures a nil interface.
// On a fetch error it returns a nil slice and a non-nil degraded response; on
// success it returns the raw results and a nil response for the caller to
// shape/truncate.
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

// okContentResponse truncates results to limit (0 = no cap) and wraps them in the
// healthy envelope — the single home for the "cap then wrap" tail every content use
// case shares.
func okContentResponse(providerName domain.ProviderName, results []domain.SearchResult, limit int) *ContentFetchResponse {
	if results == nil {
		// A provider returning (nil, nil) must still honor the non-nil Items
		// contract above, so the wire serializes [] rather than null.
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
