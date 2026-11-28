package domain

type Counters struct {
	Attempts  int64 `json:"acquire_attempts_total"`
	Wins      int64 `json:"acquire_wins_total"`
	Contended int64 `json:"acquire_contended_total"`
	Failures  int64 `json:"acquire_failures_total"`
	TermEnds  int64 `json:"term_ends_total"`
	HoldsLost int64 `json:"holds_lost_total"`
}
