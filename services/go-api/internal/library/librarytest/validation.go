package librarytest

import (
	"altune/go-api/internal/library/internal/domain"
	"errors"
	"testing"
)

func AssertValidationError(t testing.TB, err error) {
	t.Helper()
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %T (%v), want a *domain.ValidationError", err, err)
	}
}
