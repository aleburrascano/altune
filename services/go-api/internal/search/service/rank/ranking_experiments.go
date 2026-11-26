package rank

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/shared/background"
	"errors"
	"math/rand/v2"
	"slices"
)

type RankingConfig struct {
	TailDemotion        bool
	CrossKindProminence bool
	Behavior            BehavioralScorer
	ExplorationRate     float64
}

type RankingExperiments struct {
	TailDemotion bool

	CrossKindProminence bool

	behavior BehavioralScorer

	ExplorationRate float64

	bg *background.Tracker
}

var ErrBehaviorRequired = errors.New("ranking experiments need a behavioral scorer, pass NoBehavioralScoring to switch it off")

func NewRankingExperiments(cfg RankingConfig, bg *background.Tracker) (*RankingExperiments, error) {
	if cfg.Behavior == nil {
		return nil, ErrBehaviorRequired
	}
	return &RankingExperiments{
		TailDemotion:        cfg.TailDemotion,
		CrossKindProminence: cfg.CrossKindProminence,
		behavior:            cfg.Behavior,
		ExplorationRate:     cfg.ExplorationRate,
		bg:                  bg,
	}, nil
}

func (r *RankingExperiments) RankOptions() RankOptions {
	return RankOptions{
		TailDemotion:        r.TailDemotion,
		CrossKindProminence: r.CrossKindProminence,
		Behavioral:          r.behavior.Snapshot(),
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
