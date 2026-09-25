package eval

import (
	"altune/go-api/internal/discovery/domain"
	"context"
)

type CandidateRanking map[string][]string

type ReplayScore struct {
	Positives     int
	Negatives     int
	Found         int
	MRR           float64
	NegativeLeakK int
	TopK          int
}

func rankOf(order []string, sig string) int {
	for i, s := range order {
		if s == sig {
			return i
		}
	}
	return -1
}

func ReplayCorpus(corpus BehavioralCorpus, ranking CandidateRanking, topK int) ReplayScore {
	score := ReplayScore{TopK: topK}
	var rrSum float64
	for _, e := range corpus.Entries {
		order := ranking[e.Query]
		idx := rankOf(order, e.ResultSignature)
		if e.Polarity > 0 {
			score.Positives++
			if idx >= 0 {
				score.Found++
				rrSum += 1.0 / float64(idx+1)
			}
			continue
		}
		score.Negatives++
		if idx >= 0 && idx < topK {
			score.NegativeLeakK++
		}
	}
	if score.Positives > 0 {
		score.MRR = rrSum / float64(score.Positives)
	}
	return score
}

func BuildRanking(ctx context.Context, corpus BehavioralCorpus, searcher Searcher) CandidateRanking {
	ranking := CandidateRanking{}
	for _, query := range distinctQueries(corpus.Entries) {
		order, searched := rankingFromLiveSearchOrNotFound(ctx, searcher, query)
		if !searched {
			continue
		}
		ranking[query] = order
	}
	return ranking
}

func rankingFromLiveSearchOrNotFound(ctx context.Context, searcher Searcher, query string) (order []string, searched bool) {
	results, err := searcher.Search(ctx, query)
	if err != nil {
		return nil, false
	}
	order = make([]string, 0, len(results))
	for _, r := range results {
		order = append(order, domain.ResultSignature(r))
	}
	return order, true
}

func distinctQueries(entries []BehavioralCorpusEntry) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if _, ok := seen[e.Query]; ok {
			continue
		}
		seen[e.Query] = struct{}{}
		out = append(out, e.Query)
	}
	return out
}
