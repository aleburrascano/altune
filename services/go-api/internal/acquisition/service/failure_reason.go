package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"time"
)

const maxAcquisitionAttempts = 3

var (
	ErrAcquisitionRetryable = errors.New("acquisition failed transiently, job released for retry")
	ErrNoConfidentMatch     = errors.New("no candidate met the confidence floor")
	ErrReplaceFactMissing   = errors.New("replace: no fact identifies the current file")
	ErrAcquisitionTimedOut  = errors.New("acquisition exhausted its own time budget")
)

func retryBackoff(attempts int) time.Duration {
	return 30 * time.Second << (2 * (min(max(attempts, 1), maxAcquisitionAttempts) - 1))
}

func isTransientFailure(err error) bool {
	return acqdomain.IsSourceUnavailable(err) || errors.Is(err, ErrAcquisitionTimedOut)
}

func ownTimeout(jobCtx, ctx context.Context, err error) error {
	if !ports.RunTimedOut(jobCtx, ctx) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrAcquisitionTimedOut, err)
}

func failureReason(err error) string {
	return string(failureCode(err))
}

func FailureCodeOf(err error) string {
	return string(failureCode(err))
}

func failureCode(err error) shared.FailureCode {
	if errors.Is(err, ErrAcquisitionTimedOut) {
		return shared.FailureSourceUnavailable
	}
	if isCancellation(err) {
		return shared.FailureAcquisitionCancelled
	}
	if acqdomain.IsSourceUnavailable(err) {
		return shared.FailureSourceUnavailable
	}
	if errors.Is(err, acqdomain.ErrSearchDegraded) {
		return shared.FailureAcquisitionFailed
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

func foundNoSource(err error) bool {
	switch failureCode(err) {
	case shared.FailureNoMatchFound, shared.FailureNoConfidentMatch, shared.FailureDownloadFailed:
		return true
	default:
		return false
	}
}

func noSourceHasIt(err error) bool {
	switch failureCode(err) {
	case shared.FailureNoMatchFound, shared.FailureNoConfidentMatch:
		return true
	default:
		return false
	}
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
