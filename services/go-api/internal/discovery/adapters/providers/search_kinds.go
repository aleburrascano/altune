package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	attempted := 0
	var failed []domain.ResultKind
	var errs []error

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
			failed = append(failed, kind)
			errs = append(errs, err)
			continue
		}
		results = append(results, items...)
	}

	return results, kindsOutcome(provider, attempted, failed, errs)
}

func kindsOutcome(provider string, attempted int, failed []domain.ResultKind, errs []error) error {
	switch {
	case len(failed) == 0:
		return nil
	case len(failed) == attempted:
		return fmt.Errorf("%s: all %d kind searches failed: %w", provider, attempted, errs[len(errs)-1])
	default:
		return &domain.PartialKindsError{Failed: failed, Err: errors.Join(errs...)}
	}
}

func allSearchKinds() domain.ResultKindSet {
	return domain.AllKinds()
}
