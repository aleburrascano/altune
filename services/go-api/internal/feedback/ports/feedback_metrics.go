package ports

const TrackerFailureUnclassified = "unclassified"

type FeedbackMetrics interface {
	TrackerCreateFailed(cause string)
	SubmissionRejected(reason string)
	SubmissionCreated()
}

const (
	RejectUserLimit     = "user_limit"
	RejectGlobalLimit   = "global_limit"
	RejectTrackerPaused = "tracker_paused"
)
