package metrics

import (
	"altune/go-api/internal/feedback/ports"
	"testing"
)

func TestSnapshot_ReportsRejectionsAndCreations(t *testing.T) {
	before := ReadSnapshot()
	m := NewExpvarFeedbackMetrics()
	m.SubmissionRejected(ports.RejectTrackerPaused)
	m.SubmissionRejected(ports.RejectTrackerPaused)
	m.SubmissionCreated()

	after := ReadSnapshot()
	if got := after.SubmissionsRejected[ports.RejectTrackerPaused] - before.SubmissionsRejected[ports.RejectTrackerPaused]; got != 2 {
		t.Fatalf("rejected delta = %d, want 2", got)
	}
	if got := after.SubmissionsCreated - before.SubmissionsCreated; got != 1 {
		t.Fatalf("created delta = %d, want 1", got)
	}
}
