package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"fmt"
	"time"
)

const maxAcquisitionAttempts = 3

var ErrAcquisitionRetryable = errors.New("acquisition failed transiently, job released for retry")
var ErrNoConfidentMatch = errors.New("no candidate met the confidence floor")

func retryBackoff(attempts int) time.Duration {
	return 30 * time.Second << (2 * (max(attempts, 1) - 1))
}

func isTransientFailure(err error) bool {
	return ports.IsSourceUnavailable(err)
}

func failureReason(err error) string {
	return string(failureCode(err))
}

func failureCode(err error) domain.FailureCode {
	if isCancellation(err) {
		return domain.FailureAcquisitionCancelled
	}
	if ports.IsSourceUnavailable(err) {
		return domain.FailureSourceUnavailable
	}
	if errors.Is(err, ErrNoConfidentMatch) {
		return domain.FailureNoConfidentMatch
	}
	var stepErr *StepError
	if errors.As(err, &stepErr) {
		if code, ok := reasonForStep(stepErr.Step); ok {
			return code
		}
		return domain.FailureAcquisitionFailed
	}
	return domain.FailureAcquisitionFailed
}

func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

func withCancellation(ctx context.Context, err error) error {
	ctxErr := ctx.Err()
	if ctxErr == nil || errors.Is(err, ctxErr) {
		return err
	}
	return fmt.Errorf("%w (%w)", err, ctxErr)
}

func reasonForStep(step string) (domain.FailureCode, bool) {
	switch step {
	case stepNameSearch, stepNameSelect:
		return domain.FailureNoMatchFound, true
	case stepNameDownload:
		return domain.FailureDownloadFailed, true
	case stepNameStore:
		return domain.FailureStorageFailed, true
	default:
		return "", false
	}
}
