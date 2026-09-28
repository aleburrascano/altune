package metrics

import (
	"altune/go-api/internal/feedback/ports"
	"expvar"
)

const (
	TrackerCreateFailuresVar        = "feedback_tracker_create_failures_total"
	TrackerCreateFailuresByCauseVar = "feedback_tracker_create_failures_by_cause_total"
	SubmissionsCreatedVar           = "feedback_submissions_created_total"
	SubmissionsRejectedVar          = "feedback_submissions_rejected_total"
)

var (
	trackerCreateFailures        = expvar.NewInt(TrackerCreateFailuresVar)
	trackerCreateFailuresByCause = expvar.NewMap(TrackerCreateFailuresByCauseVar)
	submissionsCreated           = expvar.NewInt(SubmissionsCreatedVar)
	submissionsRejected          = expvar.NewMap(SubmissionsRejectedVar)
)

type ExpvarFeedbackMetrics struct{}

var _ ports.FeedbackMetrics = ExpvarFeedbackMetrics{}

func NewExpvarFeedbackMetrics() ExpvarFeedbackMetrics { return ExpvarFeedbackMetrics{} }

func (ExpvarFeedbackMetrics) TrackerCreateFailed(cause string) {
	trackerCreateFailures.Add(1)
	trackerCreateFailuresByCause.Add(cause, 1)
}

func (ExpvarFeedbackMetrics) SubmissionRejected(reason string) {
	submissionsRejected.Add(reason, 1)
}

func (ExpvarFeedbackMetrics) SubmissionCreated() { submissionsCreated.Add(1) }

type Snapshot struct {
	TrackerCreateFailures        int64            `json:"tracker_create_failures_total"`
	TrackerCreateFailuresByCause map[string]int64 `json:"tracker_create_failures_by_cause_total"`
	SubmissionsCreated           int64            `json:"submissions_created_total"`
	SubmissionsRejected          map[string]int64 `json:"submissions_rejected_total"`
}

func ReadSnapshot() Snapshot {
	return Snapshot{
		TrackerCreateFailures:        trackerCreateFailures.Value(),
		TrackerCreateFailuresByCause: readCounts(trackerCreateFailuresByCause),
		SubmissionsCreated:           submissionsCreated.Value(),
		SubmissionsRejected:          readCounts(submissionsRejected),
	}
}

func readCounts(m *expvar.Map) map[string]int64 {
	counts := map[string]int64{}
	m.Do(func(kv expvar.KeyValue) {
		if n, ok := kv.Value.(*expvar.Int); ok {
			counts[kv.Key] = n.Value()
		}
	})
	return counts
}
