package app

import (
	"altune/go-api/internal/discovery"
	shared "altune/go-api/internal/shared/kernel"
	"context"
)

type EvalQueryResult = discovery.SmokeQueryResult

type EvalResult = discovery.SmokeResult

type EvalRunner func(ctx context.Context) (EvalResult, error)

func (a *App) keepSearches(disc *discovery.Discovery) {
	a.searchSvc = disc.Search
	a.evalSearch = disc.RankingOnlySearch()
}

func (a *App) buildEvalRunner() EvalRunner {
	evalSvc := a.evalSearch
	if evalSvc == nil {
		return nil
	}
	return func(ctx context.Context) (EvalResult, error) {
		return discovery.RunSmokeEval(ctx, evalSvc, evalUserId())
	}
}

func evalUserId() shared.UserId {
	return shared.SystemUserId()
}
