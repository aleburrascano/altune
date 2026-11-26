package service

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/backoff"
	"altune/go-api/internal/shared/downloadfailure"
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	MaxAcquisitionAttempts = 3
)

var (
	ErrAcquisitionRetryable = errors.New("acquisition failed transiently, job released for retry")
	ErrNoConfidentMatch     = errors.New("no candidate met the confidence floor")
	ErrReplaceFactMissing   = errors.New("replace: no fact identifies the current file")
	ErrAcquisitionTimedOut  = errors.New("acquisition exhausted its own time budget")
)

const (
	jobRetryBase   = 30 * time.Second
	jobRetryFactor = 4
)

var jobRetryBackoff = backoff.MustUncapped(jobRetryBase, backoff.WithFactor(jobRetryFactor))

func retryBackoff(attempts int) time.Duration {
	return jobRetryBackoff.Ceiling(min(attempts, MaxAcquisitionAttempts))
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

func failureCode(err error) downloadfailure.FailureCode {
	if errors.Is(err, ErrAcquisitionTimedOut) {
		return downloadfailure.FailureSourceUnavailable
	}
	if isCancellation(err) {
		return downloadfailure.FailureAcquisitionCancelled
	}
	if acqdomain.IsSourceUnavailable(err) {
		return downloadfailure.FailureSourceUnavailable
	}
	if errors.Is(err, acqdomain.ErrSearchDegraded) {
		return downloadfailure.FailureAcquisitionFailed
	}
	if errors.Is(err, ErrNoConfidentMatch) {
		return downloadfailure.FailureNoConfidentMatch
	}
	var stepErr *StepError
	if !errors.As(err, &stepErr) {
		return downloadfailure.FailureAcquisitionFailed
	}
	if code, ok := reasonForStep(stepErr.Step); ok {
		return code
	}
	return downloadfailure.FailureAcquisitionFailed
}

func foundNoSource(err error) bool {
	switch failureCode(err) {
	case downloadfailure.FailureNoMatchFound, downloadfailure.FailureNoConfidentMatch, downloadfailure.FailureDownloadFailed:
		return true
	default:
		return false
	}
}

func noSourceHasIt(err error) bool {
	switch failureCode(err) {
	case downloadfailure.FailureNoMatchFound, downloadfailure.FailureNoConfidentMatch:
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

func reasonForStep(step StepName) (downloadfailure.FailureCode, bool) {
	switch step {
	case stepNameSearch, stepNameSelect:
		return downloadfailure.FailureNoMatchFound, true
	case StepNameDownload:
		return downloadfailure.FailureDownloadFailed, true
	case stepNameStore:
		return downloadfailure.FailureStorageFailed, true
	default:
		return "", false
	}
}
