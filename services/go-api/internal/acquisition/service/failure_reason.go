package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/shared"
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
	return acqdomain.IsSourceUnavailable(err)
}

func failureReason(err error) string {
	return string(failureCode(err))
}

func FailureCodeOf(err error) string {
	return string(failureCode(err))
}

func failureCode(err error) shared.FailureCode {
	if isCancellation(err) {
		return shared.FailureAcquisitionCancelled
	}
	if acqdomain.IsSourceUnavailable(err) {
		return shared.FailureSourceUnavailable
	}
	if errors.Is(err, ErrNoConfidentMatch) {
		return shared.FailureNoConfidentMatch
	}
	var stepErr *StepError
	if errors.As(err, &stepErr) {
		if code, ok := reasonForStep(stepErr.Step); ok {
			return code
		}
		return shared.FailureAcquisitionFailed
	}
	return shared.FailureAcquisitionFailed
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

func reasonForStep(step StepName) (shared.FailureCode, bool) {
	switch step {
	case stepNameSearch, stepNameSelect:
		return shared.FailureNoMatchFound, true
	case stepNameDownload:
		return shared.FailureDownloadFailed, true
	case stepNameStore:
		return shared.FailureStorageFailed, true
	default:
		return "", false
	}
}
