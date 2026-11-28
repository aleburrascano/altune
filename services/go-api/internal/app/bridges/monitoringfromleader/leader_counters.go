package monitoringfromleader

import (
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/platform/monitoring"
)

type CounterSource func() leader.Counters

type leaderReader struct {
	source CounterSource
}

func NewLeaderCounters(source CounterSource) monitoring.LeaderReader {
	return leaderReader{source: source}
}

func (r leaderReader) LeaderCounters() monitoring.LeaderCounters {
	c := r.source()
	return monitoring.LeaderCounters{
		Attempts: c.Attempts, Wins: c.Wins, Contended: c.Contended,
		Failures: c.Failures, TermEnds: c.TermEnds, HoldsLost: c.HoldsLost,
	}
}
