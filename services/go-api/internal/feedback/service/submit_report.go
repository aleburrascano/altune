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
	// IdempotencyKey is an optional caller-minted token, stable across retries
	// of one logical submission. When set, a retry of a dropped response or a
	// double-tapped Submit replays the first issue instead of creating another;
	// nil means every call creates a fresh issue.
	IdempotencyKey *string
}

type SubmitReportService struct {
	tracker     ports.IssueTracker
	metrics     ports.FeedbackMetrics
	admission   *submissionAdmission
	idempotency *submissionIdempotency
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
		tracker:     tracker,
		metrics:     metrics,
		admission:   newSubmissionAdmission(limits, now),
		idempotency: newSubmissionIdempotency(now),
	}
}

// maxIdempotencyKeyLength bounds the caller-supplied key so a hostile client
// cannot store an unbounded token. A UUID is 36 chars; 200 leaves ample room
// for other reasonable key schemes. Mirrors catalog's AddTrack key bound.
const maxIdempotencyKeyLength = 200

func validateIdempotencyKey(key *string) error {
	if key == nil {
		return nil
	}
	if *key == "" {
		return domain.NewValidationError("idempotency_key must not be empty")
	}
	if len(*key) > maxIdempotencyKeyLength {
		return domain.NewValidationError("idempotency_key exceeds maximum length")
	}
	return nil
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
	if err := validateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ports.IssueRef{}, err
	}
	return s.submit(ctx, userId, report, input.IdempotencyKey)
}

// submit routes a keyed submission through the idempotency store so a retry or
// double-tap replays the first issue; a keyless one goes straight to admission
// and create, preserving the original one-issue-per-call behaviour.
func (s *SubmitReportService) submit(
	ctx context.Context,
	userId shared.UserId,
	report *domain.Report,
	key *string,
) (ports.IssueRef, error) {
	if key == nil {
		return s.admitAndCreate(ctx, userId, report)
	}
	return s.idempotency.do(userId.String()+"\x00"+*key, func() (ports.IssueRef, error) {
		return s.admitAndCreate(ctx, userId, report)
	})
}

func (s *SubmitReportService) admitAndCreate(
	ctx context.Context,
	userId shared.UserId,
	report *domain.Report,
) (ports.IssueRef, error) {
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
