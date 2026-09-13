package ports

// FeedbackMetrics aggregates counters that reveal degradation of the external
// issue tracker, so a broken integration (an expired token, a rate limit, an
// outage) can be alerted on instead of discovered from logs.
type FeedbackMetrics interface {
	// TrackerCreateFailed records one failed IssueTracker.Create call.
	TrackerCreateFailed()
}
