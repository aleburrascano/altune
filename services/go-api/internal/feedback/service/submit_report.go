package service

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type SubmitReportInput struct {
	Kind        string
	Message     string
	Diagnostics domain.Diagnostics
}

type SubmitReportService struct {
	tracker   ports.IssueTracker
	metrics   ports.FeedbackMetrics
	admission *submissionAdmission
}

// NewSubmitReportService throttles submissions with DefaultSubmissionLimits.
func NewSubmitReportService(tracker ports.IssueTracker, metrics ports.FeedbackMetrics) *SubmitReportService {
	return NewSubmitReportServiceWithLimits(tracker, metrics, DefaultSubmissionLimits, time.Now)
}

// NewSubmitReportServiceWithLimits is NewSubmitReportService with explicit
// limits and clock.
func NewSubmitReportServiceWithLimits(
	tracker ports.IssueTracker,
	metrics ports.FeedbackMetrics,
	limits SubmissionLimits,
	now func() time.Time,
) *SubmitReportService {
	return &SubmitReportService{
		tracker:   tracker,
		metrics:   metrics,
		admission: newSubmissionAdmission(limits, now),
	}
}

func (s *SubmitReportService) Execute(
	ctx context.Context,
	userId shared.UserId,
	input SubmitReportInput,
) (ports.IssueRef, error) {
	kind, err := domain.ParseKind(input.Kind)
	if err != nil {
		return ports.IssueRef{}, err
	}
	report, err := domain.NewReport(userId, kind, input.Message, input.Diagnostics)
	if err != nil {
		return ports.IssueRef{}, err
	}
	if err := s.admission.admit(userId.String()); err != nil {
		slog.WarnContext(ctx, "feedback.throttled",
			"user_id", userId.String(),
			"reason", err.Error(),
		)
		return ports.IssueRef{}, err
	}
	return s.create(ctx, report)
}

func (s *SubmitReportService) create(ctx context.Context, report *domain.Report) (ports.IssueRef, error) {
	ref, err := s.tracker.Create(ctx, report)
	if err != nil {
		s.metrics.TrackerCreateFailed()
		slog.ErrorContext(ctx, "feedback.create_failed",
			"kind", report.Kind.String(),
			"user_id", report.Reporter.String(),
			"error", err.Error(),
		)
		return ports.IssueRef{}, fmt.Errorf("create issue: %w", err)
	}
	slog.InfoContext(ctx, "feedback.submitted",
		"issue", ref.Number,
		"kind", report.Kind.String(),
		"user_id", report.Reporter.String(),
	)
	return ref, nil
}
