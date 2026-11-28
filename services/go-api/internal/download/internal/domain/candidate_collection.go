package domain

import "fmt"

func DedupeCandidatesBySourceKey(merged, results []AudioCandidate, positionByKey map[string]int) []AudioCandidate {
	for _, c := range results {
		if !c.HasSourceKey() {
			continue
		}
		key := SourceKey(c.URL)
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
	n int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) ([]AudioCandidate, error) {
	merged, _, err := CollectCandidatesReportingOutage(n, run, onSuccess, onFailure, allFailed)
	return merged, err
}

func CollectCandidatesReportingOutage(
	n int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) (merged []AudioCandidate, outage, err error) {
	positionByKey := make(map[string]int)
	var failed searchFailures

	for i := 0; i < n; i++ {
		candidates, err := run(i)
		if err != nil {
			failed.note(err)
			onFailure(i, err)
			continue
		}
		onSuccess(i, candidates)
		merged = DedupeCandidatesBySourceKey(merged, candidates, positionByKey)
	}

	if failed.count == n && failed.first != nil {
		return nil, failed.firstUnavailable, allFailed(failed.first)
	}
	if len(merged) == 0 && failed.firstUnavailable != nil {
		return nil, failed.firstUnavailable, fmt.Errorf("no candidates and a source was unavailable: %w", failed.firstUnavailable)
	}
	if len(merged) == 0 && failed.first != nil {
		return nil, nil, degraded(failed.first)
	}
	return merged, failed.firstUnavailable, nil
}

type searchFailures struct {
	count            int
	first            error
	firstUnavailable error
}

func (f *searchFailures) note(err error) {
	f.count++
	if f.first == nil {
		f.first = err
	}
	if f.firstUnavailable == nil && IsSourceUnavailable(err) {
		f.firstUnavailable = err
	}
}

func degraded(err error) error {
	return fmt.Errorf("%w: %w", ErrSearchDegraded, err)
}
