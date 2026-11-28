package domain_test

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/httpserver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func answer(err error) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/acquisition/retry", nil), err)
	return rec
}

func TestAdmissionSentinels(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		msg    string
	}{
		{"ErrRetryNotFailed", domain.ErrRetryNotFailed, 409, "acquisition.retry_not_failed", "track is not in failed state"},
		{"ErrCooldownActive", domain.ErrCooldownActive, 429, "acquisition.cooldown_active", "cooldown active, try again later"},
		{"ErrReacquireNotReady", domain.ErrReacquireNotReady, 409, "acquisition.reacquire_not_ready", "track has no audio to replace"},
		{"ErrTrackJobInFlight", domain.ErrTrackJobInFlight, 409, "acquisition.job_in_flight", "another acquisition for this track is already running, try again later"},
		{"ErrSchedulerShutdown", domain.ErrSchedulerShutdown, 503, "acquisition.shutting_down", "acquisition is shutting down, try again later"},
		{"ErrAcquisitionPaused", domain.ErrAcquisitionPaused, 503, "acquisition.paused", "acquisition is paused, try again later"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := answer(tc.err)
			var body struct {
				Detail string `json:"detail"`
				Code   string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %s: %v", rec.Body.String(), err)
			}
			if rec.Code != tc.status || body.Code != tc.code || body.Detail != tc.msg {
				t.Errorf("answer = %d %q %q, want %d %q %q", rec.Code, body.Code, body.Detail, tc.status, tc.code, tc.msg)
			}
		})
	}
}

func TestAdmissionSentinelIdentity(t *testing.T) {
	if errors.Is(errors.New("track is not in failed state"), domain.ErrRetryNotFailed) {
		t.Error("look-alike error with the same text must not match ErrRetryNotFailed")
	}
	if !errors.Is(fmt.Errorf("x: %w", domain.ErrCooldownActive), domain.ErrCooldownActive) {
		t.Error("wrapped ErrCooldownActive must match ErrCooldownActive")
	}
}
