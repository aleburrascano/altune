package service

import (
	"context"
	"errors"
	"testing"

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
	svc := NewSubmitReportService(tracker)

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
	svc := NewSubmitReportService(tracker)

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
	svc := NewSubmitReportService(tracker)

	input := validInput()
	input.Message = "broken"
	if _, err := svc.Execute(context.Background(), newUser(), input); err == nil {
		t.Fatal("expected a short message to be rejected")
	}
	if len(tracker.reports) != 0 {
		t.Fatalf("tracker saw %d reports, want none", len(tracker.reports))
	}
}

func TestSubmitReport_NeverThrottlesAUserDumpingReports(t *testing.T) {
	tracker := &recordingTracker{}
	svc := NewSubmitReportService(tracker)
	user := newUser()

	for i := 0; i < 25; i++ {
		if _, err := svc.Execute(context.Background(), user, validInput()); err != nil {
			t.Fatalf("report %d was refused: %v", i, err)
		}
	}
	if len(tracker.reports) != 25 {
		t.Fatalf("tracker saw %d reports, want all 25", len(tracker.reports))
	}
}

func TestSubmitReport_WrapsTrackerFailure(t *testing.T) {
	tracker := &recordingTracker{err: errors.New("github is down")}
	svc := NewSubmitReportService(tracker)

	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err == nil {
		t.Fatal("expected the tracker failure to surface")
	}
}
