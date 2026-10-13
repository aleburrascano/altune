package app

import (
	"altune/go-api/internal/discovery/service/smokeeval"
	"altune/go-api/internal/shared"
	"context"
)

type EvalQueryResult = smokeeval.QueryResult

type EvalResult = smokeeval.Result

type EvalRunner func(ctx context.Context) (EvalResult, error)

func (a *App) buildEvalRunner(ctx context.Context) EvalRunner {
	if !a.cfg.EvalMeterEnabled {
		return nil
	}
	evalSvc := BuildRankingOnlySearchService(ctx, a.cfg, a.pool, a.redisClient, nil)

	return func(ctx context.Context) (EvalResult, error) {
		return smokeeval.Run(ctx, evalSvc, evalUserId())
	}
}

func evalUserId() shared.UserId {
	return shared.SystemUserId()
}
