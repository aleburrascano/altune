package domain_test

import (
	"errors"
	"fmt"
	"testing"

	acqdomain "altune/go-api/internal/acquisition/domain"
)

type admissionRefusal interface {
	error
	HTTPStatus() int
	ErrorCode() string
}

func TestAdmissionSentinels(t *testing.T) {
	cases := []struct {
		name   string
		err    admissionRefusal
		status int
		code   string
		msg    string
	}{
		{"ErrRetryNotFailed", acqdomain.ErrRetryNotFailed, 409, "acquisition.retry_not_failed", "track is not in failed state"},
		{"ErrCooldownActive", acqdomain.ErrCooldownActive, 429, "acquisition.cooldown_active", "cooldown active, try again later"},
		{"ErrReacquireNotReady", acqdomain.ErrReacquireNotReady, 409, "acquisition.reacquire_not_ready", "track has no audio to replace"},
		{"ErrTrackJobInFlight", acqdomain.ErrTrackJobInFlight, 409, "acquisition.job_in_flight", "another acquisition for this track is already running, try again later"},
		{"ErrSchedulerShutdown", acqdomain.ErrSchedulerShutdown, 503, "acquisition.shutting_down", "acquisition is shutting down, try again later"},
		{"ErrAcquisitionPaused", acqdomain.ErrAcquisitionPaused, 503, "acquisition.paused", "acquisition is paused, try again later"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.HTTPStatus(); got != tc.status {
				t.Errorf("HTTPStatus = %d, want %d", got, tc.status)
			}
			if got := tc.err.ErrorCode(); got != tc.code {
				t.Errorf("ErrorCode = %q, want %q", got, tc.code)
			}
			if got := tc.err.Error(); got != tc.msg {
				t.Errorf("Error = %q, want %q", got, tc.msg)
			}
		})
	}
}

func TestAdmissionSentinelIdentity(t *testing.T) {
	if errors.Is(errors.New("track is not in failed state"), acqdomain.ErrRetryNotFailed) {
		t.Error("look-alike error with the same text must not match ErrRetryNotFailed")
	}
	if !errors.Is(fmt.Errorf("x: %w", acqdomain.ErrCooldownActive), acqdomain.ErrCooldownActive) {
		t.Error("wrapped ErrCooldownActive must match ErrCooldownActive")
	}
}
