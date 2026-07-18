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
	ProviderName string
	Status       domain.ProviderStatus
	Items        []domain.SearchResult
}

// errorContentResponse is the degraded envelope every content use case returns
// when a provider is missing, unparseable, or fails: an error status with a
// non-nil empty item slice (so the wire serializes [] rather than null).
func errorContentResponse(providerName string) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusError,
		Items:        []domain.SearchResult{},
	}
}

// emptyContentResponse is the OK-but-nothing-found envelope: a healthy fetch that
// produced no items, again with a non-nil empty slice for the wire.
func emptyContentResponse(providerName string) *ContentFetchResponse {
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        []domain.SearchResult{},
	}
}

// fetchProviderResults runs the "provider lookup → parse name → fetch → warn-degrade"
// prefix shared by the content use cases (top-tracks, albums, related). found is the
// caller's typed-map lookup result — the maps hold different provider port types, so
// the lookup stays at the call site while the degrade contract lives here. On any
// failure it returns a nil slice and a non-nil degraded response; on success it
// returns the raw results and a nil response for the caller to shape/truncate.
func fetchProviderResults(
	ctx context.Context,
	providerName, externalID, logKey string,
	found bool,
	fetch func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error),
) ([]domain.SearchResult, *ContentFetchResponse) {
	if !found {
		return nil, errorContentResponse(providerName)
	}
	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return nil, errorContentResponse(providerName)
	}
	results, err := fetch(ctx, pn, externalID)
	if err != nil {
		slog.WarnContext(ctx, logKey,
			"provider", providerName, "external_id", externalID, "error", err)
		return nil, errorContentResponse(providerName)
	}
	return results, nil
}

// okContentResponse truncates results to limit (0 = no cap) and wraps them in the
// healthy envelope — the single home for the "cap then wrap" tail every content use
// case shares.
func okContentResponse(providerName string, results []domain.SearchResult, limit int) *ContentFetchResponse {
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        results,
	}
}
