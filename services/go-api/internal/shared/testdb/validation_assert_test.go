package testdb_test

import (
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/testdb"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

type recordingTB struct {
	testing.TB
	failure string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.failure = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func assertValidation(t *testing.T, err error) (*apierror.CodedError, string) {
	t.Helper()
	tb := &recordingTB{TB: t}
	var got *apierror.CodedError
	done := make(chan struct{})
	go func() {
		defer close(done)
		got = testdb.AssertValidationError(tb, err)
	}()
	<-done
	return got, tb.failure
}

func TestAssertValidationError_AcceptsAnInvalidKindAndReturnsIt(t *testing.T) {
	want := apierror.NewError(apierror.CodeRequestInvalid, "bad body")

	got, failure := assertValidation(t, fmt.Errorf("decode: %w", want))

	if failure != "" || got != want {
		t.Fatalf("got %v with failure %q, want the wrapped coded error and no failure", got, failure)
	}
}

func TestAssertValidationError_RefusesACodedErrorOfAnotherKind(t *testing.T) {
	_, failure := assertValidation(t, apierror.NewError(apierror.CodeInternal, "boom"))

	if !strings.Contains(failure, "want invalid") {
		t.Fatalf("failure %q, want it to name the invalid kind it expected", failure)
	}
}

func TestAssertValidationError_RefusesAnErrorThatIsNotCoded(t *testing.T) {
	_, failure := assertValidation(t, errors.New("plain"))

	if !strings.Contains(failure, "want a *apierror.CodedError") {
		t.Fatalf("failure %q, want it to name the coded error it expected", failure)
	}
}
