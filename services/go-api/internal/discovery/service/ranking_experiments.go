package service

import (
	"altune/go-api/internal/discovery/domain"
	"math/rand/v2"
	"slices"
	"sync/atomic"
)

// rankingExperiments is the exploration / bandit-ranking collaborator. It holds
// the eval-gated ranking rungs (tail-noise demotion, cross-kind prominence,
// behavioral re-ranking) plus the exploration coin-flip and the behavioral
// score snapshot those rungs read. It used to be promoted, inline state on
// Service (an embedded struct); pulling it into a named unit lets a
// ranking-experiment change stay off the search orchestrator.
type rankingExperiments struct {
	tailDemotion bool

	crossKindProminence bool

	behavioralRanking  bool
	behavioralConsumer *SatisfactionConsumer
	behavioralScores   atomic.Pointer[map[string]float64]

	explorationRate float64

	bg *backgroundRunner
}

// rankOptions projects the enabled experiments onto the RankOptions the rank
// pipeline consumes, keeping the experiment-to-pipeline mapping in one place.
func (r *rankingExperiments) rankOptions() RankOptions {
	return RankOptions{
		TailDemotion:        r.tailDemotion,
		CrossKindProminence: r.crossKindProminence,
		Behavioral:          r.behavioralScoresSnapshot(),
	}
}

// maybeExplore, at the configured rate, returns a shuffled clone of the top page
// so the search can occasionally probe below the greedy order. It never mutates
// the input slice (which may be a cached list) and never explores fewer than two
// results.
func (r *rankingExperiments) maybeExplore(ranked []domain.SearchResult) ([]domain.SearchResult, bool) {
	if r.explorationRate <= 0 || len(ranked) < 2 {
		return ranked, false
	}
	if rand.Float64() >= r.explorationRate {
		return ranked, false
	}
	out := slices.Clone(ranked)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out, true
}
