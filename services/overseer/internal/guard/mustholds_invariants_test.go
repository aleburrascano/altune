package guard_test

import "testing"

func TestSpentRefreshTokenSignsInAgainWithNoHumanStep(t *testing.T) {
	t.Skip("lands in [Task]: overseer signs the read-only account in again when its refresh token dies (#2352)")
}

func TestPasswordGrantRunsOncePerBackoffAndNeverLogsThePassword(t *testing.T) {
	t.Skip("lands in [Task]: overseer signs the read-only account in again when its refresh token dies (#2352)")
}

func TestFailedCredentialNeverMarksGoAPIDown(t *testing.T) {
	t.Skip("lands in [Task]: overseer probes go-api public health without a token (#2357)")
}

func TestDegradedGoAPIShowsTheRealDownDependency(t *testing.T) {
	t.Skip("lands in [Bug]: overseer reliability mirror goes stale when go-api degrades (#2054)")
}

func TestStreamDropShowsConnectingAndASilentStreamReconnects(t *testing.T) {
	t.Skip("lands in [Task]: overseer streams show connecting on drop and reconnect when silent (#2355)")
}

func TestAtMostOneTokenInvalidationPerAccessToken(t *testing.T) {
	t.Skip("lands in [Task]: overseer invalidates a token once per 401 burst (#2358)")
}

func TestBlueGreenFlipIsReadWithNoRestart(t *testing.T) {
	t.Skip("lands in [Task]: overseer reads go-api through an internal caddy listener (#2361)")
}

func TestSnapshotAndSeriesJSONMatchTheWebTypes(t *testing.T) {
	t.Skip("lands in [Task]: overseer contract test pins go snapshot and series json to the web types (#2387)")
}
