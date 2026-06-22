package providers

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
)

// defaultKindOrder is the deterministic order kind-fan-out adapters attempt
// their per-kind searches in. Fixed (not map iteration) so a provider's native
// result ordering — which feeds the RRF position tiebreak in Rank — is stable
// run-to-run.
var defaultKindOrder = []domain.ResultKind{
	domain.ResultKindArtist,
	domain.ResultKindTrack,
	domain.ResultKindAlbum,
}

// searchAcrossKinds is the single home for the per-kind fan-out that every
// kind-iterating SearchProvider needs: run searchOne for each requested+supported
// kind, aggregate the results, and surface an error ONLY when every attempted
// kind failed. That last rule is load-bearing — it lets the circuit breaker see a
// total provider outage while a partial mix (some kinds ok, some failed) still
// ships. Before this, each adapter hand-rolled the loop and the partial/outage
// decision; the deletion test concentrates that complexity here.
func searchAcrossKinds(
	ctx context.Context,
	provider, query string,
	requested, supported map[domain.ResultKind]bool,
	searchOne func(ctx context.Context, kind domain.ResultKind) ([]domain.SearchResult, error),
) ([]domain.SearchResult, error) {
	var results []domain.SearchResult
	attempted, failed := 0, 0
	var lastErr error

	for _, kind := range defaultKindOrder {
		if !requested[kind] || !supported[kind] {
			continue
		}

		attempted++
		items, err := searchOne(ctx, kind)
		if err != nil {
			slog.WarnContext(ctx, provider+".search_kind_failed",
				"kind", kind.String(), "query", query, "error", err)
			failed++
			lastErr = err
			continue
		}
		results = append(results, items...)
	}

	if attempted > 0 && failed == attempted {
		return nil, fmt.Errorf("%s: all %d kind searches failed: %w", provider, attempted, lastErr)
	}
	return results, nil
}
