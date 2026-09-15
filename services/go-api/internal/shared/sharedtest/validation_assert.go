// Package sharedtest holds test-support helpers for internal/shared value
// objects. It is a separate package (rather than a _test.go file in
// internal/shared) so the production build of internal/shared never links the
// stdlib testing package, while domain and service tests can still share the
// helper via the domain-purity depguard allow-list.
package sharedtest

import (
	"altune/go-api/internal/shared"
	"errors"
	"testing"
)

// AssertValidationError asserts that err is, or wraps, a *shared.ValidationError
// and returns it for further assertions. It uses errors.As so the check stays
// correct when the error is wrapped upstream; the test is failed otherwise.
func AssertValidationError(t testing.TB, err error) *shared.ValidationError {
	t.Helper()
	var ve *shared.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %T (%v), want *shared.ValidationError", err, err)
	}
	return ve
}
