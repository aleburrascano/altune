package providers

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
)

var defaultKindOrder = []domain.ResultKind{
	domain.ResultKindArtist,
	domain.ResultKindTrack,
	domain.ResultKindAlbum,
}

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
				"kind", kind.String(), logging.SearchTextAttr(query),
				"error", redact.Secrets(logging.ScrubSearchErr(err, query)))
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
