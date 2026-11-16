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

const GoldenPass = goldeneval.Pass
