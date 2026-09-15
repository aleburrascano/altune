package service

import (
	"altune/go-api/internal/catalog/domain"
	"errors"
	"strings"
)

// failureReason maps a pipeline error to the stable failure code persisted as
// the track's failure_reason; the catalog derives the user-facing message from
// it. Internal error text never reaches the code. It returns the code as a
// string because callers append a rejection summary after
// domain.FailureDetailSeparator.
func failureReason(err error) string {
	return string(failureCode(err))
}

func failureCode(err error) domain.FailureCode {
	var stepErr *StepError
	if errors.As(err, &stepErr) {
		if code, ok := reasonForStep(stepErr.Step); ok {
			return code
		}
		return domain.FailureAcquisitionFailed
	}
	if strings.HasPrefix(err.Error(), "pipeline cancelled") {
		return domain.FailureAcquisitionCancelled
	}
	return domain.FailureAcquisitionFailed
}

func reasonForStep(step string) (domain.FailureCode, bool) {
	switch step {
	case "search", "select":
		return domain.FailureNoMatchFound, true
	case "download":
		return domain.FailureDownloadFailed, true
	case "store":
		return domain.FailureStorageFailed, true
	default:
		return "", false
	}
}
