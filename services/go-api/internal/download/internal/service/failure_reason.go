package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/music"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/resilience"
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

var jobRetryBackoff = resilience.MustUncappedBackoff(jobRetryBase, resilience.WithBackoffFactor(jobRetryFactor), resilience.WithBackoffSpread(jobRetrySpread))

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

func failureCode(err error) music.FailureCode {
	if errors.Is(err, ErrAcquisitionTimedOut) {
		return music.FailureSourceUnavailable
	}
	if isCancellation(err) {
		return music.FailureAcquisitionCancelled
	}
	if domain.IsSourceUnavailable(err) {
		return music.FailureSourceUnavailable
	}
	if errors.Is(err, domain.ErrSearchDegraded) {
		return music.FailureAcquisitionFailed
	}
	if errors.Is(err, ErrNoConfidentMatch) {
		return music.FailureNoConfidentMatch
	}
	var stepErr *StepError
	if !errors.As(err, &stepErr) {
		return music.FailureAcquisitionFailed
	}
	if code, ok := reasonForStep(stepErr.Step); ok {
		return code
	}
	return music.FailureAcquisitionFailed
}

func foundNoSource(err error) bool {
	switch failureCode(err) {
	case music.FailureNoMatchFound, music.FailureNoConfidentMatch, music.FailureDownloadFailed:
		return true
	case music.FailureSourceUnavailable,
		music.FailureStorageFailed,
		music.FailureAcquisitionCancelled,
		music.FailureAcquisitionFailed,
		music.FailureYtdlpError,
		music.FailureAcquisitionInterrupted,
		music.FailureAcquisitionRefused:
		return false
	}
	return false
}

func noSourceHasIt(err error) bool {
	switch failureCode(err) {
	case music.FailureNoMatchFound, music.FailureNoConfidentMatch:
		return true
	case music.FailureSourceUnavailable,
		music.FailureDownloadFailed,
		music.FailureStorageFailed,
		music.FailureAcquisitionCancelled,
		music.FailureAcquisitionFailed,
		music.FailureYtdlpError,
		music.FailureAcquisitionInterrupted,
		music.FailureAcquisitionRefused:
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

func reasonForStep(step StepName) (music.FailureCode, bool) {
	switch step {
	case stepNameSearch, stepNameSelect:
		return music.FailureNoMatchFound, true
	case StepNameDownload:
		return music.FailureDownloadFailed, true
	case stepNameStore:
		return music.FailureStorageFailed, true
	case stepNameTag, stepNameUpdateTrack, stepNamePipeline:
		return "", false
	}
	return "", false
}
