package shared

import (
	"errors"
	"testing"
)

// AssertValidationError asserts that err is, or wraps, a *ValidationError and
// returns it for further assertions. It uses errors.As so the check stays
// correct when the error is wrapped upstream; the test is failed otherwise.
//
// It lives beside ValidationError (rather than in a per-context test package)
// because the domain packages may import internal/shared but not test-helper
// packages, so this is the one spot every catalog test can share.
func AssertValidationError(t testing.TB, err error) *ValidationError {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %T (%v), want *shared.ValidationError", err, err)
	}
	return ve
}
