package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/random"
	"errors"
	"slices"
)

type RankingConfig struct {
	TailDemotion        bool
	CrossKindProminence bool
	Behavior            BehavioralScorer
	ExplorationRate     float64
	ExplorationDraw     random.Unit
}

type RankingExperiments struct {
	TailDemotion bool

	CrossKindProminence bool

	behavior BehavioralScorer

	ExplorationRate float64
	explorationDraw random.Unit

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
		explorationDraw:     cfg.ExplorationDraw,
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
	if r.explorationDraw() >= r.ExplorationRate {
		return ranked, false
	}
	out := slices.Clone(ranked)
	for i := len(out) - 1; i > 0; i-- {
		j := min(int(r.explorationDraw()*float64(i+1)), i)
		out[i], out[j] = out[j], out[i]
	}
	return out, true
}
