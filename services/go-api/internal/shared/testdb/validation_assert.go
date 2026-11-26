package testdb

import (
	apierror "altune/go-api/internal/shared/apierror"
	"errors"
	"testing"
)

func AssertValidationError(t testing.TB, err error) *apierror.CodedError {
	t.Helper()
	var coded *apierror.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %T (%v), want a *apierror.CodedError", err, err)
	}
	if kind := coded.Code().Kind(); kind != apierror.KindInvalid {
		t.Fatalf("error %v has kind %s, want %s", err, kind, apierror.KindInvalid)
	}
	return coded
}
