package app

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/ids"
	"context"
)

type EvalQueryResult = search.SmokeQueryResult

type EvalResult = search.SmokeResult

type EvalRunner func(ctx context.Context) (EvalResult, error)

func (a *App) keepSearches(disc *search.Discovery) {
	a.evalSearch = disc.RankingOnlySearch()
}

func (a *App) buildEvalRunner() EvalRunner {
	evalSvc := a.evalSearch
	return func(ctx context.Context) (EvalResult, error) {
		return search.RunSmokeEval(ctx, evalSvc, evalUserId())
	}
}

func evalUserId() ids.UserId {
	return ids.SystemUserId()
}
