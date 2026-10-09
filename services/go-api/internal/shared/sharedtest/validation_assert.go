package sharedtest

import (
	shareddomain "altune/go-api/internal/shared/domain"
	"errors"
	"testing"
)

func AssertValidationError(t testing.TB, err error) *shareddomain.ValidationError {
	t.Helper()
	var ve *shareddomain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %T (%v), want *shareddomain.ValidationError", err, err)
	}
	return ve
}
