package app

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/ids"
	"context"
)

type evalRunner func(ctx context.Context) (search.SmokeResult, error)

func (a *application) keepSearches(disc *search.Discovery) {
	a.evalSearch = disc.RankingOnlySearch()
}

func (a *application) buildEvalRunner() evalRunner {
	evalSvc := a.evalSearch
	return func(ctx context.Context) (search.SmokeResult, error) {
		return search.RunSmokeEval(ctx, evalSvc, evalUserId())
	}
}

func evalUserId() ids.UserId {
	return ids.SystemUserId()
}
