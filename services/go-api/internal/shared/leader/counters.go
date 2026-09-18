package leader

import "sync/atomic"

// electionCounters tallies the outcome of every election tick. Read without a
// lock while the election loop writes, so each tally is its own atomic: an
// operator view may straddle a tick, which is acceptable for a trend.
type electionCounters struct {
	attempts  atomic.Int64
	wins      atomic.Int64
	contended atomic.Int64
	failures  atomic.Int64
	termEnds  atomic.Int64
}

// Counters is a point-in-time read of one election's tallies. Failures climbing
// while Wins and Contended stand still is an instance shut out of the election
// by the database rather than by a rival — the case a live IsLeader() boolean
// cannot tell from a healthy standby. Wins climbing alongside TermEnds is a
// leader flapping between instances.
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
