package service

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"sync"
	"testing"
	"time"
)

type slowTracker struct {
	latency       time.Duration
	mu            sync.Mutex
	creates       int
	correlationID string
	deadline      time.Duration
}

func (s *slowTracker) Create(ctx context.Context, _ *domain.Report) (ports.IssueRef, error) {
	s.record(ctx)
	select {
	case <-time.After(s.latency):
		return ports.IssueRef{Number: 7, URL: "https://github.com/o/r/issues/7"}, nil
	case <-ctx.Done():
		return ports.IssueRef{}, ctx.Err()
	}
}

func (s *slowTracker) record(ctx context.Context) {
	deadline, _ := ctx.Deadline()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creates++
	s.correlationID = logging.CorrelationIDFromContext(ctx)
	s.deadline = time.Until(deadline)
}

func cancelledMidCreate(t *testing.T, correlationID string) context.Context {
	ctx, cancel := context.WithCancel(logging.WithCorrelationID(context.Background(), correlationID))
	timer := time.AfterFunc(50*time.Millisecond, cancel)
	t.Cleanup(func() { timer.Stop(); cancel() })
	return ctx
}

func TestSubmitReport_CreateOutlivesACancelledRequestAndItsRetryReplays(t *testing.T) {
	tracker := &slowTracker{latency: 200 * time.Millisecond}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})
	user := newUser()

	first, err := svc.Execute(cancelledMidCreate(t, "corr-a"), user, keyedInput())
	retry, retryErr := svc.Execute(context.Background(), user, keyedInput())

	if err != nil || retryErr != nil {
		t.Fatalf("Execute errs = %v, %v; want the issue from both", err, retryErr)
	}
	if first.Number != 7 || retry != first {
		t.Fatalf("first = %+v, retry = %+v; want issue 7 replayed", first, retry)
	}
	if tracker.creates != 1 {
		t.Fatalf("tracker creates = %d, want 1", tracker.creates)
	}
}

func TestSubmitReport_DetachedCreateKeepsCorrelationAndItsOwnDeadline(t *testing.T) {
	tracker := &slowTracker{latency: time.Millisecond}
	svc := NewSubmitReportService(tracker, &recordingMetrics{})

	_, err := svc.Execute(logging.WithCorrelationID(context.Background(), "corr-b"), newUser(), validInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tracker.correlationID != "corr-b" {
		t.Fatalf("tracker correlation id = %q, want corr-b", tracker.correlationID)
	}
	if tracker.deadline <= 14*time.Second || tracker.deadline > trackerCreateTimeout {
		t.Fatalf("tracker deadline in %v, want about %v", tracker.deadline, trackerCreateTimeout)
	}
}
