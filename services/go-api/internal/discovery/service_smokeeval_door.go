package discovery

import smokeeval "altune/go-api/internal/discovery/service/smokeeval"

type (
	SmokeQueryResult = smokeeval.QueryResult
	SmokeResult      = smokeeval.Result
)

var RunSmokeEval = smokeeval.Run
