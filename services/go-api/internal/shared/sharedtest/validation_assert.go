package sharedtest

import (
	shareddomain "altune/go-api/internal/shared/domain"
	"errors"
	"testing"
)

func AssertValidationError(t testing.TB, err error) *shareddomain.CodedError {
	t.Helper()
	var coded *shareddomain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %T (%v), want a *shareddomain.CodedError", err, err)
	}
	if kind := coded.Code().Kind(); kind != shareddomain.KindInvalid {
		t.Fatalf("error %v has kind %s, want %s", err, kind, shareddomain.KindInvalid)
	}
	return coded
}
