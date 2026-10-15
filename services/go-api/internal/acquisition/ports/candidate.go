package ports

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"fmt"
	"math"
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
	Title      string
	Duration   float64
	URL        string
	Channel    string
	Categories []string
	ViewCount  int64
	Source     acqdomain.SourceName
	Resolved   bool
	Explicit   bool
	Unplayable string
	Artists    []string
	Album      string
	Kind       CandidateKind
}

func DedupeCandidatesBySourceKey(merged, results []AudioCandidate, positionByKey map[string]int) []AudioCandidate {
	for _, c := range results {
		if c.URL == "" {
			continue
		}
		merged = mergeCandidate(merged, c, positionByKey)
	}
	return merged
}

func mergeCandidate(merged []AudioCandidate, c AudioCandidate, positionByKey map[string]int) []AudioCandidate {
	key := acqdomain.SourceKey(c.URL)
	position, isDuplicate := positionByKey[key]
	if !isDuplicate {
		positionByKey[key] = len(merged)
		return append(merged, c)
	}
	if c.Resolved && !merged[position].Resolved {
		merged[position] = c
	}
	return merged
}

const EnoughCandidates = 8

const unlimitedCandidates = math.MaxInt

func CollectCandidates(
	n int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) ([]AudioCandidate, error) {
	return CollectCandidatesUntilEnough(n, unlimitedCandidates, run, onSuccess, onFailure, allFailed)
}

func CollectCandidatesUntilEnough(
	n, enough int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) ([]AudioCandidate, error) {
	merged, _, err := CollectCandidatesReportingOutage(n, enough, run, onSuccess, onFailure, allFailed)
	return merged, err
}

func CollectCandidatesReportingOutage(
	n, enough int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) (merged []AudioCandidate, outage error, err error) {
	positionByKey := make(map[string]int)
	var firstErr, firstUnavailable error
	failures := 0

	for i := 0; i < n && len(merged) < enough; i++ {
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
		merged = DedupeCandidatesBySourceKey(merged, candidates, positionByKey)
	}

	if failures == n && firstErr != nil {
		return nil, firstUnavailable, allFailed(firstErr)
	}
	if len(merged) == 0 && firstUnavailable != nil {
		return nil, firstUnavailable, fmt.Errorf("no candidates and a source was unavailable: %w", firstUnavailable)
	}
	return merged, firstUnavailable, nil
}
