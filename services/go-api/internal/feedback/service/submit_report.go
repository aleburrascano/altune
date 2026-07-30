package service

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared"
)

type SubmitReportInput struct {
	Kind        string
	Message     string
	Diagnostics domain.Diagnostics
}

type SubmitReportService struct {
	tracker ports.IssueTracker
	limiter *RateLimiter
}

func NewSubmitReportService(tracker ports.IssueTracker, limiter *RateLimiter) *SubmitReportService {
	return &SubmitReportService{tracker: tracker, limiter: limiter}
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
	if err := s.limiter.Allow(userId); err != nil {
		return ports.IssueRef{}, err
	}
	return s.create(ctx, report)
}

func (s *SubmitReportService) create(ctx context.Context, report *domain.Report) (ports.IssueRef, error) {
	ref, err := s.tracker.Create(ctx, report)
	if err != nil {
		return ports.IssueRef{}, fmt.Errorf("create issue: %w", err)
	}
	slog.InfoContext(ctx, "feedback.submitted",
		"issue", ref.Number,
		"kind", report.Kind.String(),
		"user_id", report.Reporter.String(),
	)
	return ref, nil
}
