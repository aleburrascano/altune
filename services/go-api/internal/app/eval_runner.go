package app

import (
	"altune/go-api/internal/discovery"
	shared "altune/go-api/internal/shared/kernel"
	"context"
)

type EvalQueryResult = discovery.SmokeQueryResult

type EvalResult = discovery.SmokeResult

type EvalRunner func(ctx context.Context) (EvalResult, error)

func (a *App) buildEvalRunner(ctx context.Context) EvalRunner {
	if !a.cfg.EvalMeterEnabled {
		return nil
	}
	evalSvc := BuildRankingOnlySearchService(ctx, a.cfg, a.pool, a.redisClient, nil)

	return func(ctx context.Context) (EvalResult, error) {
		return discovery.RunSmokeEval(ctx, evalSvc, evalUserId())
	}
}

func evalUserId() shared.UserId {
	return shared.SystemUserId()
}
