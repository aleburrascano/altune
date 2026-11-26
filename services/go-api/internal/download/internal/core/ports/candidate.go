package ports

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/sourcekey"
	"context"
	"fmt"
	"log/slog"
)

const (
	UnplayableDRM     = "drm"
	UnplayablePreview = "preview"
)

type CandidateKind string

const (
	KindArtTrack    CandidateKind = "art_track"
	KindPinnedVideo CandidateKind = "pinned_video"
	KindUpload      CandidateKind = "upload"
	KindCatalogPin  CandidateKind = "catalog_pin"
)

type AudioCandidate struct {
	Title       string
	Duration    float64
	URL         string
	Channel     string
	Categories  []string
	ViewCount   int64
	Source      acqdomain.SourceName
	Explicit    bool
	Unplayable  string
	Artists     []string
	Album       string
	Kind        CandidateKind
	ObservedOMV bool
}

func (c AudioCandidate) IsResolved() bool {
	return c.Kind != "" && c.Kind != KindUpload
}

func DedupeCandidatesBySourceKey(ctx context.Context, merged, results []AudioCandidate, positionByKey map[string]int) []AudioCandidate {
	for _, c := range results {
		key := sourcekey.SourceKey(c.URL)
		if key == "" {
			slog.WarnContext(ctx, "acquisition.candidate_dropped_no_source_key", "url", c.URL, "source", c.Source)
			continue
		}
		merged = mergeCandidate(merged, c, key, positionByKey)
	}
	return merged
}

func mergeCandidate(merged []AudioCandidate, c AudioCandidate, key string, positionByKey map[string]int) []AudioCandidate {
	position, isDuplicate := positionByKey[key]
	if !isDuplicate {
		positionByKey[key] = len(merged)
		return append(merged, c)
	}
	if c.IsResolved() && !merged[position].IsResolved() {
		merged[position] = c
	}
	return merged
}

func CollectCandidates(
	ctx context.Context,
	n int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) ([]AudioCandidate, error) {
	merged, _, err := CollectCandidatesReportingOutage(ctx, n, run, onSuccess, onFailure, allFailed)
	return merged, err
}

func CollectCandidatesReportingOutage(
	ctx context.Context,
	n int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) (merged []AudioCandidate, outage, err error) {
	positionByKey := make(map[string]int)
	var firstErr, firstUnavailable error
	failures := 0

	for i := 0; i < n; i++ {
		candidates, err := run(i)
		if err != nil {
			failures++
			if firstErr == nil {
				firstErr = err
			}
			if firstUnavailable == nil && acqdomain.IsSourceUnavailable(err) {
				firstUnavailable = err
			}
			onFailure(i, err)
			continue
		}
		onSuccess(i, candidates)
		merged = DedupeCandidatesBySourceKey(ctx, merged, candidates, positionByKey)
	}

	if failures == n && firstErr != nil {
		return nil, firstUnavailable, allFailed(firstErr)
	}
	if len(merged) == 0 && firstUnavailable != nil {
		return nil, firstUnavailable, fmt.Errorf("no candidates and a source was unavailable: %w", firstUnavailable)
	}
	if len(merged) == 0 && firstErr != nil {
		return nil, nil, degraded(firstErr)
	}
	return merged, firstUnavailable, nil
}

func degraded(err error) error {
	return fmt.Errorf("%w: %w", acqdomain.ErrSearchDegraded, err)
}
