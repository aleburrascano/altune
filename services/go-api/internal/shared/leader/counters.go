package leader

import "sync/atomic"

type electionCounters struct {
	attempts  atomic.Int64
	wins      atomic.Int64
	contended atomic.Int64
	failures  atomic.Int64
	termEnds  atomic.Int64
}

type Counters struct {
	Attempts  int64 `json:"acquire_attempts_total"`
	Wins      int64 `json:"acquire_wins_total"`
	Contended int64 `json:"acquire_contended_total"`
	Failures  int64 `json:"acquire_failures_total"`
	TermEnds  int64 `json:"term_ends_total"`
}

func (c *electionCounters) read() Counters {
	return Counters{
		Attempts:  c.attempts.Load(),
		Wins:      c.wins.Load(),
		Contended: c.contended.Load(),
		Failures:  c.failures.Load(),
		TermEnds:  c.termEnds.Load(),
	}
}
