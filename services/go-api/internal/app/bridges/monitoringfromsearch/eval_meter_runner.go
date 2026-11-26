package monitoringfromsearch

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"context"
)

func NewEvalMeterRunner(run func(context.Context) (search.SmokeResult, error)) monitoring.EvalMeterRunner {
	if run == nil {
		return nil
	}
	return func(ctx context.Context) (monitoring.EvalMeterResult, error) {
		res, err := run(ctx)
		if err != nil {
			return monitoring.EvalMeterResult{}, err
		}
		return meterResult(res), nil
	}
}

func meterResult(res search.SmokeResult) monitoring.EvalMeterResult {
	queries := make([]monitoring.EvalMeterQueryResult, len(res.Queries))
	for i, q := range res.Queries {
		queries[i] = monitoring.EvalMeterQueryResult{
			Query:    q.Query,
			Expect:   q.Expect,
			Passed:   q.Passed,
			Position: q.Position,
		}
	}
	return monitoring.EvalMeterResult{
		Score:     res.Score,
		Baseline:  res.Baseline,
		Regressed: res.Regressed,
		Errored:   res.Errored,
		Queries:   queries,
	}
}
