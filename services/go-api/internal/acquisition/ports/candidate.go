package ports

import "math"

type AudioCandidate struct {
	Title      string
	Duration   float64
	URL        string
	Channel    string
	Categories []string
	ViewCount  int64
	Source     string
	Resolved   bool
}

func DedupeCandidatesByURL(merged, results []AudioCandidate, seen map[string]bool) []AudioCandidate {
	for _, c := range results {
		if c.URL == "" || seen[c.URL] {
			continue
		}
		seen[c.URL] = true
		merged = append(merged, c)
	}
	return merged
}

// EnoughCandidates mirrors the acquisition service's download-attempt cap: a
// search that has merged this many has nothing left to win, because the ranking
// downstream never reaches past that many candidates.
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

// CollectCandidatesUntilEnough stops running sources once enough candidates have
// merged. Sources are ordered best-first, so the runs it skips would each have
// paid their own process spawn and timeout to grow a tail the caller never
// reads.
func CollectCandidatesUntilEnough(
	n, enough int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) ([]AudioCandidate, error) {
	seen := make(map[string]bool)
	var merged []AudioCandidate
	var firstErr error
	failures := 0

	for i := 0; i < n && len(merged) < enough; i++ {
		candidates, err := run(i)
		if err != nil {
			failures++
			if firstErr == nil {
				firstErr = err
			}
			onFailure(i, err)
			continue
		}
		onSuccess(i, candidates)
		merged = DedupeCandidatesByURL(merged, candidates, seen)
	}

	if failures == n && firstErr != nil {
		return nil, allFailed(firstErr)
	}
	return merged, nil
}
