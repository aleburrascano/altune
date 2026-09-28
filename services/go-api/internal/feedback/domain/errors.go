package domain

import "altune/go-api/internal/shared"

type ValidationError = shared.ValidationError

func NewValidationError(msg string) *ValidationError {
	return shared.NewValidationError("feedback", msg)
}
