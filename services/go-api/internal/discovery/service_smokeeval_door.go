package discovery

import smokeeval "altune/go-api/internal/discovery/internal/shell/smokeeval"

type (
	SmokeQueryResult = smokeeval.QueryResult
	SmokeResult      = smokeeval.Result
)

var RunSmokeEval = smokeeval.Run
