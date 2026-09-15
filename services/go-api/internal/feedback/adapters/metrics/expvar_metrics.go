// Package metrics provides an expvar-backed implementation of the feedback
// metrics port. The counters are process-global published integers that can be
// scraped from an expvar endpoint.
package metrics

import (
	"altune/go-api/internal/feedback/ports"
	"expvar"
)

// TrackerCreateFailuresVar is the published expvar name for failed issue
// tracker creates.
const TrackerCreateFailuresVar = "feedback_tracker_create_failures_total"

// Declared at package scope because expvar.NewInt panics on a duplicate name;
// registering once keeps the adapter safe to construct any number of times.
var trackerCreateFailures = expvar.NewInt(TrackerCreateFailuresVar)

// ExpvarFeedbackMetrics implements ports.FeedbackMetrics by incrementing
// process-global expvar counters.
type ExpvarFeedbackMetrics struct{}

var _ ports.FeedbackMetrics = ExpvarFeedbackMetrics{}

// NewExpvarFeedbackMetrics returns an ExpvarFeedbackMetrics.
func NewExpvarFeedbackMetrics() ExpvarFeedbackMetrics { return ExpvarFeedbackMetrics{} }

func (ExpvarFeedbackMetrics) TrackerCreateFailed() { trackerCreateFailures.Add(1) }

// Snapshot is a point-in-time read of the feedback counters, shaped for JSON
// exposure.
type Snapshot struct {
	TrackerCreateFailures int64 `json:"tracker_create_failures_total"`
}

// ReadSnapshot returns the current values of the published feedback counters. It
// is a read-only accessor over the package-scope expvar var so callers can
// expose this specific counter without reaching the raw expvar registry (which
// also publishes process globals like cmdline and memstats).
func ReadSnapshot() Snapshot {
	return Snapshot{TrackerCreateFailures: trackerCreateFailures.Value()}
}
