package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"token acquisition failed", &goapi.TokenError{Op: "acquire read-only token", Err: errors.New("refresh token spent")}, "auth"},
		{"401 after retry", &goapi.APIError{Op: "GET /health", StatusCode: http.StatusUnauthorized}, "auth"},
		{"403", &goapi.APIError{Op: "GET /observe/health", StatusCode: http.StatusForbidden}, "auth"},
		{"429", &goapi.APIError{Op: "GET /health", StatusCode: http.StatusTooManyRequests}, "throttled"},
		{"503", &goapi.APIError{Op: "GET /health", StatusCode: http.StatusServiceUnavailable}, "degraded"},
		{"500", &goapi.APIError{Op: "GET /health", StatusCode: http.StatusInternalServerError}, "down"},
		{"dial error", &goapi.SourceDownError{Op: "GET /health", Err: errors.New("dial tcp: connection refused")}, "down"},
		{"context deadline", &goapi.SourceDownError{Op: "GET /health", Err: context.DeadlineExceeded}, "down"},
		{"unrecognised 404", &goapi.APIError{Op: "GET /health", StatusCode: http.StatusNotFound}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := goapi.Classify(tc.err); got != tc.want {
				t.Errorf("Classify(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
