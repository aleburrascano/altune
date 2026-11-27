package monitoring

import evalmeter "altune/go-api/internal/platform/monitoring/service/searchqualitycheck"

type (
	EvalMeter            = evalmeter.Meter
	EvalMeterQueryResult = evalmeter.QueryResult
	EvalMeterResult      = evalmeter.Result
	EvalMeterRunner      = evalmeter.Runner
)

const EvalMeterInterval = evalmeter.DefaultInterval

var (
	NewEvalMeter               = evalmeter.New
	ErrEvalMeterClockRequired  = evalmeter.ErrClockRequired
	ErrEvalMeterRunnerRequired = evalmeter.ErrRunnerRequired
	ErrEvalMeterPausedRequired = evalmeter.ErrPausedRequired
)
