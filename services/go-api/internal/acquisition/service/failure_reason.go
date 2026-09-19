package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"fmt"
)

// failureReason maps a pipeline error to the stable failure code persisted as
// the track's failure_reason; the catalog derives the user-facing message from
// it. Internal error text never reaches the code. It returns the code as a
// string because callers append a rejection summary after
// domain.FailureDetailSeparator.
func failureReason(err error) string {
	return string(failureCode(err))
}

// failureCode classifies what the error says before the step it happened in: a
// job whose context ended mid-step did not fail permanently, and a source that
// never answered is evidence about the source, not about the track. Only once
// neither holds does the step decide, where search means "searched and found
// nothing" and download means "the audio would not come down".
func failureCode(err error) domain.FailureCode {
	if isCancellation(err) {
		return domain.FailureAcquisitionCancelled
	}
	if ports.IsSourceUnavailable(err) {
		return domain.FailureSourceUnavailable
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

// isCancellation classifies purely on the wrapped context error, never on
// message text: runStage wraps ctx.Err() with %w ("pipeline cancelled: %w")
// and every step routes its failures through withCancellation, so errors.Is
// reaches the cause. Matching a "pipeline cancelled" prefix would silently
// break the moment that wording changed.
func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// withCancellation attaches ctx's cancellation cause to a step's error when
// the step failed after its context ended. Adapters often return their own
// error (or an empty result) without wrapping ctx.Err(), which failureCode
// would otherwise classify as the step's permanent failure.
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
