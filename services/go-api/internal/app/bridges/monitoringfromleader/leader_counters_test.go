package monitoringfromleader_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromleader"
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/platform/monitoring"
	"testing"
)

func TestLeaderCounters_CarriesEveryCounter(t *testing.T) {
	source := func() leader.Counters {
		return leader.Counters{Attempts: 4, Wins: 3, Contended: 1, Failures: 7, TermEnds: 2, HoldsLost: 5}
	}

	got := monitoringfromleader.NewLeaderCounters(source).LeaderCounters()

	want := monitoring.LeaderCounters{Attempts: 4, Wins: 3, Contended: 1, Failures: 7, TermEnds: 2, HoldsLost: 5}
	if got != want {
		t.Errorf("counters = %+v, want %+v", got, want)
	}
}
