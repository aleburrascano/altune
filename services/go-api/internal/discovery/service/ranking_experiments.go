package service

import (
	"altune/go-api/internal/discovery/domain"
	"math/rand/v2"
	"slices"
	"sync/atomic"
)

type rankingConfig struct {
	tailDemotion        bool
	crossKindProminence bool
	behavioralRanking   bool
	behavioralConsumer  *SatisfactionConsumer
	explorationRate     float64
}

type RankingExperiments struct {
	tailDemotion bool

	crossKindProminence bool

	behavioralRanking  bool
	behavioralConsumer *SatisfactionConsumer
	behavioralScores   atomic.Pointer[map[string]float64]

	explorationRate float64

	bg *backgroundRunner
}

func newRankingExperiments(cfg rankingConfig, bg *backgroundRunner) *RankingExperiments {
	return &RankingExperiments{
		tailDemotion:        cfg.tailDemotion,
		crossKindProminence: cfg.crossKindProminence,
		behavioralRanking:   cfg.behavioralRanking,
		behavioralConsumer:  cfg.behavioralConsumer,
		explorationRate:     cfg.explorationRate,
		bg:                  bg,
	}
}

func (r *RankingExperiments) rankOptions() RankOptions {
	return RankOptions{
		TailDemotion:        r.tailDemotion,
		CrossKindProminence: r.crossKindProminence,
		Behavioral:          r.behavioralScoresSnapshot(),
	}
}

func (r *RankingExperiments) maybeExplore(ranked []domain.SearchResult) ([]domain.SearchResult, bool) {
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
