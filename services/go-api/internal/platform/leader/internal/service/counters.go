package service

import (
	"altune/go-api/internal/platform/leader/internal/domain"
	"sync/atomic"
)

type electionCounters struct {
	attempts  atomic.Int64
	wins      atomic.Int64
	contended atomic.Int64
	failures  atomic.Int64
	termEnds  atomic.Int64
	holdsLost atomic.Int64
}

func (c *electionCounters) read() domain.Counters {
	return domain.Counters{
		Attempts:  c.attempts.Load(),
		Wins:      c.wins.Load(),
		Contended: c.contended.Load(),
		Failures:  c.failures.Load(),
		TermEnds:  c.termEnds.Load(),
		HoldsLost: c.holdsLost.Load(),
	}
}
