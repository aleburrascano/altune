package search

import smokeeval "altune/go-api/internal/search/internal/shell/searchsmoketest"

type (
	SmokeQueryResult = smokeeval.QueryResult
	SmokeResult      = smokeeval.Result
)

var RunSmokeEval = smokeeval.Run
