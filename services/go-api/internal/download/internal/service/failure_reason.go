package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/backoff"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/random"
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
	jobRetrySpread = 0.2
)

var jobRetryBackoff = backoff.MustUncapped(jobRetryBase, backoff.WithFactor(jobRetryFactor), backoff.WithSpread(jobRetrySpread))

func retryBackoff(attempts int, unit random.Unit) time.Duration {
	return jobRetryBackoff.DelayAt(min(attempts, MaxAcquisitionAttempts), unit())
}

func isTransientFailure(err error) bool {
	return domain.IsSourceUnavailable(err) || errors.Is(err, ErrAcquisitionTimedOut)
}

var errAcquireBudgetSpent = errors.New("acquisition budget spent")

func ownBudgetSpent(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errAcquireBudgetSpent)
}

func ownTimeout(ctx context.Context, err error) error {
	if !ownBudgetSpent(ctx) {
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
	if domain.IsSourceUnavailable(err) {
		return downloadfailure.FailureSourceUnavailable
	}
	if errors.Is(err, domain.ErrSearchDegraded) {
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
	case downloadfailure.FailureSourceUnavailable,
		downloadfailure.FailureStorageFailed,
		downloadfailure.FailureAcquisitionCancelled,
		downloadfailure.FailureAcquisitionFailed,
		downloadfailure.FailureYtdlpError,
		downloadfailure.FailureAcquisitionInterrupted,
		downloadfailure.FailureAcquisitionRefused:
		return false
	}
	return false
}

func noSourceHasIt(err error) bool {
	switch failureCode(err) {
	case downloadfailure.FailureNoMatchFound, downloadfailure.FailureNoConfidentMatch:
		return true
	case downloadfailure.FailureSourceUnavailable,
		downloadfailure.FailureDownloadFailed,
		downloadfailure.FailureStorageFailed,
		downloadfailure.FailureAcquisitionCancelled,
		downloadfailure.FailureAcquisitionFailed,
		downloadfailure.FailureYtdlpError,
		downloadfailure.FailureAcquisitionInterrupted,
		downloadfailure.FailureAcquisitionRefused:
		return false
	}
	return false
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
	case stepNameTag, stepNameUpdateTrack, stepNamePipeline:
		return "", false
	}
	return "", false
}
