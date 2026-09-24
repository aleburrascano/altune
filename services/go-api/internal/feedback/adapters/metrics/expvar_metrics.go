// Package metrics provides an expvar-backed implementation of the feedback
// metrics port. The counters are process-global published integers that can be
// scraped from an expvar endpoint.
package metrics

import (
	"altune/go-api/internal/feedback/ports"
	"expvar"
)

// Published expvar names for failed issue tracker creates: the overall total
// and its per-cause breakdown.
const (
	TrackerCreateFailuresVar        = "feedback_tracker_create_failures_total"
	TrackerCreateFailuresByCauseVar = "feedback_tracker_create_failures_by_cause_total"
	SubmissionsCreatedVar           = "feedback_submissions_created_total"
	SubmissionsRejectedVar          = "feedback_submissions_rejected_total"
)

// Declared at package scope because expvar.NewInt/NewMap panic on a duplicate
// name; registering once keeps the adapter safe to construct any number of
// times.
var (
	trackerCreateFailures        = expvar.NewInt(TrackerCreateFailuresVar)
	trackerCreateFailuresByCause = expvar.NewMap(TrackerCreateFailuresByCauseVar)
	submissionsCreated           = expvar.NewInt(SubmissionsCreatedVar)
	submissionsRejected          = expvar.NewMap(SubmissionsRejectedVar)
)

// ExpvarFeedbackMetrics implements ports.FeedbackMetrics by incrementing
// process-global expvar counters.
type ExpvarFeedbackMetrics struct{}

var _ ports.FeedbackMetrics = ExpvarFeedbackMetrics{}

// NewExpvarFeedbackMetrics returns an ExpvarFeedbackMetrics.
func NewExpvarFeedbackMetrics() ExpvarFeedbackMetrics { return ExpvarFeedbackMetrics{} }

// TrackerCreateFailed bumps both the overall failure count (the one number to
// alert on) and the per-cause breakdown (to tell a dead token or wrong repo,
// which needs an operator, from a GitHub outage, which passes on its own).
func (ExpvarFeedbackMetrics) TrackerCreateFailed(cause string) {
	trackerCreateFailures.Add(1)
	trackerCreateFailuresByCause.Add(cause, 1)
}

func (ExpvarFeedbackMetrics) SubmissionRejected(reason string) {
	submissionsRejected.Add(reason, 1)
}

func (ExpvarFeedbackMetrics) SubmissionCreated() { submissionsCreated.Add(1) }

// Snapshot is a point-in-time read of the feedback counters, shaped for JSON
// exposure.
type Snapshot struct {
	TrackerCreateFailures        int64            `json:"tracker_create_failures_total"`
	TrackerCreateFailuresByCause map[string]int64 `json:"tracker_create_failures_by_cause_total"`
	SubmissionsCreated           int64            `json:"submissions_created_total"`
	SubmissionsRejected          map[string]int64 `json:"submissions_rejected_total"`
}

// ReadSnapshot returns the current values of the published feedback counters. It
// is a read-only accessor over the package-scope expvar var so callers can
// expose these specific counters without reaching the raw expvar registry (which
// also publishes process globals like cmdline and memstats).
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
