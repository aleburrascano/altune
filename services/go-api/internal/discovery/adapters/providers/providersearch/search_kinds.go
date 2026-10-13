package providersearch

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

func SearchAcrossKinds(
	ctx context.Context,
	provider, query string,
	requested, supported map[domain.ResultKind]bool,
	searchOne func(ctx context.Context, kind domain.ResultKind) ([]domain.SearchResult, error),
) ([]domain.SearchResult, error) {
	var run kindsRun
	for _, kind := range defaultKindOrder {
		if requested[kind] && supported[kind] {
			items, err := searchOne(ctx, kind)
			run.record(ctx, provider, query, kind, items, err)
		}
	}
	return run.results, KindsOutcome(provider, run.attempted, run.failed, run.errs)
}

type kindsRun struct {
	results   []domain.SearchResult
	attempted int
	failed    []domain.ResultKind
	errs      []error
}

func (k *kindsRun) record(
	ctx context.Context,
	provider, query string,
	kind domain.ResultKind,
	items []domain.SearchResult,
	err error,
) {
	k.attempted++
	if err != nil {
		slog.WarnContext(ctx, provider+".search_kind_failed",
			"kind", kind.String(), logging.SearchTextAttr(query),
			"error", redact.Secrets(logging.ScrubSearchErr(err, query)))
		k.failed = append(k.failed, kind)
		k.errs = append(k.errs, err)
		return
	}
	k.results = append(k.results, items...)
}

func KindsOutcome(provider string, attempted int, failed []domain.ResultKind, errs []error) error {
	switch {
	case len(failed) == 0:
		return nil
	case len(failed) == attempted:
		return fmt.Errorf("%s: all %d kind searches failed: %w", provider, attempted, errs[len(errs)-1])
	default:
		return &domain.PartialKindsError{Failed: failed, Err: errors.Join(errs...)}
	}
}

func AllSearchKinds() domain.ResultKindSet {
	return domain.AllKinds()
}
