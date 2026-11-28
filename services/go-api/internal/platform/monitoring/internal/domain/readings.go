package domain

type LatencyBucket struct {
	LeMs  string
	Count uint64
}

type StatusClasses struct {
	Count2xx uint64
	Count4xx uint64
	Count5xx uint64
}

type RouteLatency struct {
	Count   uint64
	SumMs   uint64
	Buckets []LatencyBucket
	Status  StatusClasses
}

type RequestLatency struct {
	Routes map[string]RouteLatency
}

type LeaderCounters struct {
	Attempts  int64
	Wins      int64
	Contended int64
	Failures  int64
	TermEnds  int64
	HoldsLost int64
}

type JobState struct {
	ConsecutiveFailures int64
	Disabled            bool
}
