package ports

// TrackerFailureUnclassified is the cause recorded for a failed
// IssueTracker.Create whose error carries no wire code (e.g. a lost 201
// confirmation), so every failure still lands under exactly one cause.
const TrackerFailureUnclassified = "unclassified"

// FeedbackMetrics aggregates counters that reveal degradation of the external
// issue tracker, so a broken integration (an expired token, a rate limit, an
// outage) can be alerted on instead of discovered from logs.
type FeedbackMetrics interface {
	// TrackerCreateFailed records one failed IssueTracker.Create call. cause is
	// the tracker error's wire code (a closed set defined by the tracker
	// adapter, so it is safe to key a counter by) or TrackerFailureUnclassified.
	TrackerCreateFailed(cause string)
	SubmissionRejected(reason string)
	SubmissionCreated()
}

const (
	RejectUserLimit     = "user_limit"
	RejectGlobalLimit   = "global_limit"
	RejectTrackerPaused = "tracker_paused"
)
