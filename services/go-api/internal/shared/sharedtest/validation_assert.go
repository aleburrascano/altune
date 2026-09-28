package sharedtest

import (
	"altune/go-api/internal/shared"
	"errors"
	"testing"
)

func AssertValidationError(t testing.TB, err error) *shared.ValidationError {
	t.Helper()
	var ve *shared.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %T (%v), want *shared.ValidationError", err, err)
	}
	return ve
}
