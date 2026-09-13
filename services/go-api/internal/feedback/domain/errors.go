package domain

import "altune/go-api/internal/shared"

// ValidationError aliases the shared type so this package keeps one name for
// its 400s while the implementation lives in internal/shared.
type ValidationError = shared.ValidationError

// NewValidationError builds a feedback validation error
// ("feedback.validation_error").
func NewValidationError(msg string) *ValidationError {
	return shared.NewValidationError("feedback", msg)
}
