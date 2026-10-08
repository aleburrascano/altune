package domain

import (
	"net/http"
	"testing"
)

func TestStreamUnavailable(t *testing.T) {
	logs := StreamUnavailable("logs")
	if logs.Error() != "logs stream unavailable" || logs.HTTPStatus() != http.StatusServiceUnavailable || logs.ErrorCode() != "observe.stream_unavailable" {
		t.Fatalf("unexpected logs error: %+v", logs)
	}
	events := StreamUnavailable("events")
	if events.Error() != "events stream unavailable" {
		t.Fatalf("message = %q", events.Error())
	}
	if events.HTTPStatus() != logs.HTTPStatus() || events.ErrorCode() != logs.ErrorCode() {
		t.Fatalf("events differs from logs beyond message: %+v", events)
	}
}

func TestErrPrincipalRequiredKeepsForbidden(t *testing.T) {
	if ErrPrincipalRequired.HTTPStatus() != http.StatusForbidden {
		t.Fatalf("status = %d", ErrPrincipalRequired.HTTPStatus())
	}
	if ErrPrincipalRequired.ErrorCode() != "observe.principal_required" {
		t.Fatalf("code = %q", ErrPrincipalRequired.ErrorCode())
	}
}
