package service

import (
	"errors"
	"strings"
)

func failureReason(err error) string {
	var stepErr *StepError
	if errors.As(err, &stepErr) {
		if reason, ok := reasonForStep(stepErr.Step); ok {
			return reason
		}
		return "audio acquisition failed"
	}
	if strings.HasPrefix(err.Error(), "pipeline cancelled") {
		return "audio acquisition cancelled"
	}
	return "audio acquisition failed"
}

func reasonForStep(step string) (string, bool) {
	switch step {
	case "search", "select":
		return "no matching audio found", true
	case "download":
		return "audio download failed", true
	case "store":
		return "audio storage failed", true
	default:
		return "", false
	}
}
