package observe

import evalmeter "altune/go-api/internal/observe/service/evalmeter"

type (
	EvalMeter            = evalmeter.Meter
	EvalMeterQueryResult = evalmeter.QueryResult
	EvalMeterResult      = evalmeter.Result
	EvalMeterRunner      = evalmeter.Runner
)

var NewEvalMeter = evalmeter.New
