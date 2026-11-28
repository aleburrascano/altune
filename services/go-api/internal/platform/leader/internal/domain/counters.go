package domain

type Counters struct {
	Attempts  int64
	Wins      int64
	Contended int64
	Failures  int64
	TermEnds  int64
	HoldsLost int64
}
