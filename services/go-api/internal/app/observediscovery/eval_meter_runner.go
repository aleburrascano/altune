package observediscovery

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"context"
)

func NewEvalMeterRunner(run func(context.Context) (discovery.SmokeResult, error)) observe.EvalMeterRunner {
	if run == nil {
		return nil
	}
	return func(ctx context.Context) (observe.EvalMeterResult, error) {
		res, err := run(ctx)
		if err != nil {
			return observe.EvalMeterResult{}, err
		}
		return meterResult(res), nil
	}
}

func meterResult(res discovery.SmokeResult) observe.EvalMeterResult {
	queries := make([]observe.EvalMeterQueryResult, len(res.Queries))
	for i, q := range res.Queries {
		queries[i] = observe.EvalMeterQueryResult{
			Query:    q.Query,
			Expect:   q.Expect,
			Passed:   q.Passed,
			Position: q.Position,
		}
	}
	return observe.EvalMeterResult{
		Score:     res.Score,
		Baseline:  res.Baseline,
		Regressed: res.Regressed,
		Errored:   res.Errored,
		Queries:   queries,
	}
}
