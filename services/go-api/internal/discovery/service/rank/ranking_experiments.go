package rank

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/runloop"
	"math/rand/v2"
	"slices"
	"sync/atomic"
)

type RankingConfig struct {
	TailDemotion        bool
	CrossKindProminence bool
	BehavioralRanking   bool
	BehavioralConsumer  *SatisfactionConsumer
	ExplorationRate     float64
}

type RankingExperiments struct {
	TailDemotion bool

	CrossKindProminence bool

	BehavioralRanking  bool
	BehavioralConsumer *SatisfactionConsumer
	behavioralScores   atomic.Pointer[map[string]float64]

	ExplorationRate float64

	bg *runloop.Tracker
}

func NewRankingExperiments(cfg RankingConfig, bg *runloop.Tracker) *RankingExperiments {
	return &RankingExperiments{
		TailDemotion:        cfg.TailDemotion,
		CrossKindProminence: cfg.CrossKindProminence,
		BehavioralRanking:   cfg.BehavioralRanking,
		BehavioralConsumer:  cfg.BehavioralConsumer,
		ExplorationRate:     cfg.ExplorationRate,
		bg:                  bg,
	}
}

func (r *RankingExperiments) RankOptions() RankOptions {
	return RankOptions{
		TailDemotion:        r.TailDemotion,
		CrossKindProminence: r.CrossKindProminence,
		Behavioral:          r.behavioralScoresSnapshot(),
	}
}

func (r *RankingExperiments) MaybeExplore(ranked []domain.SearchResult) ([]domain.SearchResult, bool) {
	if r.ExplorationRate <= 0 || len(ranked) < 2 {
		return ranked, false
	}
	if rand.Float64() >= r.ExplorationRate {
		return ranked, false
	}
	out := slices.Clone(ranked)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out, true
}
