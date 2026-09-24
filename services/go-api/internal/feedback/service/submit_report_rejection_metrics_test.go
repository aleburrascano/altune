package service

import (
	"altune/go-api/internal/feedback/ports"
	"context"
	"fmt"
	"testing"
)

type outcomeMetrics struct {
	rejected map[string]int
	created  int
}

func (m *outcomeMetrics) TrackerCreateFailed(string) {}
func (m *outcomeMetrics) SubmissionCreated()         { m.created++ }
func (m *outcomeMetrics) SubmissionRejected(reason string) {
	m.rejected[reason]++
}

func TestSubmitReport_CountsEveryRejectionDuringTrackerPause(t *testing.T) {
	metrics := &outcomeMetrics{rejected: map[string]int{}}
	throttle := fmt.Errorf("github issues: %w", uncreatedThrottleErr{uncreatedErr{code: "tracker_rate_limited"}})
	tracker := &recordingTracker{err: throttle}
	svc, _ := throttledService(tracker)
	svc.metrics = metrics

	_, _ = svc.Execute(context.Background(), newUser(), validInput())
	for i := 0; i < 5; i++ {
		_, _ = svc.Execute(context.Background(), newUser(), validInput())
	}

	if got := metrics.rejected[ports.RejectTrackerPaused]; got != 5 {
		t.Fatalf("tracker_paused rejections = %d, want 5", got)
	}
}

func TestSubmitReport_CountsUserLimitRejectionsAndCreations(t *testing.T) {
	metrics := &outcomeMetrics{rejected: map[string]int{}}
	svc, _ := throttledService(&recordingTracker{})
	svc.metrics = metrics
	user := newUser()

	for i := 0; i < testLimits.PerUser+2; i++ {
		_, _ = svc.Execute(context.Background(), user, validInput())
	}

	if metrics.created != testLimits.PerUser || metrics.rejected[ports.RejectUserLimit] != 2 {
		t.Fatalf("created=%d rejected=%v", metrics.created, metrics.rejected)
	}
}
