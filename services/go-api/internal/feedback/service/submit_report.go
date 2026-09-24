package service

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"
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

const trackerCreateTimeout = 15 * time.Second

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
		return ports.IssueRef{}, logRejection(ctx, userId, err, rejectUnknownKind,
			"kind_runes", utf8.RuneCountInString(input.Kind))
	}
	report, err := domain.NewReport(userId, kind, input.Message, input.Diagnostics)
	if err != nil {
		return ports.IssueRef{}, logRejection(ctx, userId, err, rejectInvalidReport,
			"kind", kind.String(), "message_runes", utf8.RuneCountInString(input.Message))
	}
	if err := validateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ports.IssueRef{}, logRejection(ctx, userId, err, rejectInvalidIdempotencyKey,
			"kind", kind.String(), "idempotency_key_bytes", keyBytes(input.IdempotencyKey))
	}
	return s.submit(ctx, userId, report, input.IdempotencyKey)
}

// Rejection reasons are a fixed vocabulary so a wave of malformed submissions
// can be counted by cause from logs alone.
const (
	rejectUnknownKind           = "unknown_kind"
	rejectInvalidReport         = "invalid_report"
	rejectInvalidIdempotencyKey = "invalid_idempotency_key"
)

// logRejection records a validation rejection and returns err unchanged. It
// deliberately logs only the user, a fixed reason, the error code and
// shape attributes (lengths, a parsed kind): never the error text or the raw
// input, because a report can hold pasted secrets that are redacted only once
// accepted, and ParseKind's error echoes the submitted kind.
func logRejection(ctx context.Context, userId shared.UserId, err error, reason string, shape ...any) error {
	attrs := append([]any{"user_id", userId.String(), "reason", reason}, shape...)
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		attrs = append(attrs, "error_code", coded.ErrorCode())
	}
	slog.WarnContext(ctx, "feedback.rejected", attrs...)
	return err
}

func keyBytes(key *string) int {
	if key == nil {
		return 0
	}
	return len(*key)
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
	slot, err := s.admission.admit(userId.String())
	if err != nil {
		slog.WarnContext(ctx, "feedback.throttled",
			"user_id", userId.String(),
			"reason", err.Error(),
		)
		return ports.IssueRef{}, err
	}
	return s.create(ctx, report, slot)
}

func (s *SubmitReportService) create(ctx context.Context, report *domain.Report, slot quotaSlot) (ports.IssueRef, error) {
	ref, err := s.createOutlivingRequest(ctx, report)
	s.admission.observe(ctx, err)
	if err != nil {
		cause := trackerFailureCause(err)
		s.metrics.TrackerCreateFailed(cause)
		refunded := refundable(err) && s.admission.refund(slot)
		slog.ErrorContext(ctx, "feedback.create_failed",
			"kind", report.Kind.String(),
			"user_id", report.Reporter.String(),
			"cause", cause,
			"quota_refunded", refunded,
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

func (s *SubmitReportService) createOutlivingRequest(ctx context.Context, report *domain.Report) (ports.IssueRef, error) {
	createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), trackerCreateTimeout)
	defer cancel()
	return s.tracker.Create(createCtx, report)
}

// refundable reports whether a failed create may hand its quota slot back: the
// tracker must vouch that no issue exists, and it must not be a throttle, whose
// request GitHub counted against the token (the throttle pause handles those).
func refundable(err error) bool {
	var uncreated ports.TrackerUncreated
	if !errors.As(err, &uncreated) || !uncreated.Uncreated() {
		return false
	}
	var throttle ports.TrackerThrottle
	if errors.As(err, &throttle) {
		if _, throttled := throttle.Throttled(); throttled {
			return false
		}
	}
	return true
}

// trackerFailureCause names why a tracker create failed: the error's wire code
// when the adapter classified it, else TrackerFailureUnclassified.
func trackerFailureCause(err error) string {
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		return coded.ErrorCode()
	}
	return ports.TrackerFailureUnclassified
}
