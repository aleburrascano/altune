package search

import smokeeval "altune/go-api/internal/search/internal/shell/smokeeval"

type (
	SmokeQueryResult = smokeeval.QueryResult
	SmokeResult      = smokeeval.Result
)

var RunSmokeEval = smokeeval.Run
