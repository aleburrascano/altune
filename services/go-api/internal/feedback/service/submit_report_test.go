package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type recordingTracker struct {
	reports []*domain.Report
	err     error
}

func (t *recordingTracker) Create(_ context.Context, report *domain.Report) (ports.IssueRef, error) {
	if t.err != nil {
		return ports.IssueRef{}, t.err
	}
	t.reports = append(t.reports, report)
	return ports.IssueRef{Number: 42, URL: "https://github.com/o/r/issues/42"}, nil
}

func newUser() shared.UserId { return shared.NewUserId(uuid.New()) }

func validInput() SubmitReportInput {
	return SubmitReportInput{Kind: "bug", Message: "three downloaded tracks went grey again"}
}

func TestSubmitReport_CreatesIssueAndReturnsRef(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, NewRateLimiter(5, time.Hour))

	ref, err := svc.Execute(context.Background(), newUser(), validInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ref.Number != 42 {
		t.Fatalf("issue number = %d, want 42", ref.Number)
	}
	if len(tracker.reports) != 1 {
		t.Fatalf("tracker saw %d reports, want 1", len(tracker.reports))
	}
}

func TestSubmitReport_RejectsUnknownKindBeforeCallingTracker(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, NewRateLimiter(5, time.Hour))

	input := validInput()
	input.Kind = "rant"
	if _, err := svc.Execute(context.Background(), newUser(), input); err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
	if len(tracker.reports) != 0 {
		t.Fatalf("tracker saw %d reports, want none", len(tracker.reports))
	}
}

func TestSubmitReport_RejectsShortMessageBeforeCallingTracker(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, NewRateLimiter(5, time.Hour))

	input := validInput()
	input.Message = "broken"
	if _, err := svc.Execute(context.Background(), newUser(), input); err == nil {
		t.Fatal("expected a short message to be rejected")
	}
	if len(tracker.reports) != 0 {
		t.Fatalf("tracker saw %d reports, want none", len(tracker.reports))
	}
}

func TestSubmitReport_RateLimitsPerUser(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, NewRateLimiter(2, time.Hour))
	user := newUser()

	for i := 0; i < 2; i++ {
		if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
	}

	_, err := svc.Execute(context.Background(), user, validInput())
	var limited *RateLimitedError
	if !errors.As(err, &limited) {
		t.Fatalf("err = %v, want a RateLimitedError", err)
	}
	if limited.HTTPStatus() != 429 {
		t.Fatalf("status = %d, want 429", limited.HTTPStatus())
	}
	if limited.RetryAfter <= 0 {
		t.Fatalf("retry after = %s, want a positive delay", limited.RetryAfter)
	}
}

func TestSubmitReport_RateLimitIsNotSharedBetweenUsers(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, NewRateLimiter(1, time.Hour))

	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err != nil {
		t.Fatalf("first user: %v", err)
	}
	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err != nil {
		t.Fatalf("second user: %v", err)
	}
}

func TestSubmitReport_DoesNotSpendQuotaOnRejectedInput(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker, NewRateLimiter(1, time.Hour))
	user := newUser()

	input := validInput()
	input.Message = "nope"
	if _, err := svc.Execute(context.Background(), user, input); err == nil {
		t.Fatal("expected a short message to be rejected")
	}
	if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
		t.Fatalf("a rejected report must not spend quota: %v", err)
	}
}

func TestSubmitReport_WrapsTrackerFailure(t *testing.T) {
	tracker := &recordingTracker{err: errors.New("github is down")}
	svc := NewSubmitReportService(tracker, NewRateLimiter(5, time.Hour))

	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err == nil {
		t.Fatal("expected the tracker failure to surface")
	}
}

func TestRateLimiter_ForgetsOlderThanTheWindow(t *testing.T) {
	limiter := NewRateLimiter(1, time.Hour)
	now := time.Now().UTC()
	limiter.now = func() time.Time { return now }
	user := newUser()

	if err := limiter.Allow(user); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := limiter.Allow(user); err == nil {
		t.Fatal("expected the second call inside the window to be limited")
	}

	limiter.now = func() time.Time { return now.Add(time.Hour + time.Minute) }
	if err := limiter.Allow(user); err != nil {
		t.Fatalf("call after the window: %v", err)
	}
}
