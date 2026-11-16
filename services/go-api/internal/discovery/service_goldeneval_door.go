package discovery

import goldeneval "altune/go-api/internal/discovery/service/goldeneval"

type (
	GoldenCase   = goldeneval.Case
	GoldenReport = goldeneval.Report
)

var (
	IsMBID           = goldeneval.IsMBID
	ParseGoldenCases = goldeneval.Parse
	RunGoldenEval    = goldeneval.Run
)

const (
	GoldenErrored      = goldeneval.Errored
	GoldenFail         = goldeneval.Fail
	GoldenFixed        = goldeneval.Fixed
	GoldenPass         = goldeneval.Pass
	GoldenStillFailing = goldeneval.StillFailing
)
