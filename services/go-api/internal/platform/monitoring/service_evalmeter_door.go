package monitoring

import evalmeter "altune/go-api/internal/platform/monitoring/service/searchqualitycheck"

type (
	EvalMeter            = evalmeter.Meter
	EvalMeterQueryResult = evalmeter.QueryResult
	EvalMeterResult      = evalmeter.Result
	EvalMeterRunner      = evalmeter.Runner
)

var (
	NewEvalMeter                   = evalmeter.New
	ErrEvalMeterRunnerRequired     = evalmeter.ErrRunnerRequired
	ErrEvalMeterLeadershipRequired = evalmeter.ErrLeadershipRequired
)
