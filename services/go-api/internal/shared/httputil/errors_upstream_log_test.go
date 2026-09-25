package httputil

import (
	"altune/go-api/internal/shared/logging"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type wrappingStatusTestError struct {
	status int
	code   string
	cause  error
}

func (e *wrappingStatusTestError) Error() string        { return e.cause.Error() }
func (e *wrappingStatusTestError) Unwrap() error        { return e.cause }
func (e *wrappingStatusTestError) HTTPStatus() int      { return e.status }
func (e *wrappingStatusTestError) ErrorCode() string    { return e.code }
func (e *wrappingStatusTestError) ClientDetail() string { return "upstream unavailable" }

func captureLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logging.Setup("error", false)
}

func recordsNamed(ring *logging.RingBuffer, message string) []logging.CapturedRecord {
	var named []logging.CapturedRecord
	for _, captured := range ring.Snapshot() {
		if captured.Message == message {
			named = append(named, captured)
		}
	}
	return named
}

func onlyRecord(t *testing.T, ring *logging.RingBuffer, message string) logging.CapturedRecord {
	t.Helper()
	named := recordsNamed(ring, message)
	if len(named) != 1 {
		t.Fatalf("%s records = %d, want 1", message, len(named))
	}
	return named[0]
}

func TestHandleServiceError_5xxStatusErrorLogsWrappedCause(t *testing.T) {
	ring := captureLogs(t)
	cause := errors.New("dial tcp api.github.com: connection refused")
	err := fmt.Errorf("create issue: %w", &wrappingStatusTestError{http.StatusBadGateway, "tracker_unreachable", cause})
	rec := httptest.NewRecorder()

	HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/v1/feedback", nil), err)

	logged := onlyRecord(t, ring, "service.upstream_error")
	if logged.Level != "ERROR" {
		t.Errorf("level = %s, want ERROR", logged.Level)
	}
	want := map[string]string{"method": "POST", "path": "/v1/feedback", "status": "502", "code": "tracker_unreachable"}
	for key, value := range want {
		if logged.Attrs[key] != value {
			t.Errorf("attr %s = %q, want %q", key, logged.Attrs[key], value)
		}
	}
	if !strings.Contains(logged.Attrs["error"], cause.Error()) {
		t.Errorf("error attr = %q, want it to carry the cause %q", logged.Attrs["error"], cause.Error())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"detail":"upstream unavailable","code":"tracker_unreachable"}` {
		t.Errorf("body = %s, want client detail unchanged", got)
	}
}

func TestHandleServiceError_4xxStatusErrorLogsNoUpstreamError(t *testing.T) {
	ring := captureLogs(t)
	err := &wrappingStatusTestError{http.StatusNotFound, "catalog.track_not_found", errors.New("no such track")}

	HandleServiceError(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil), err)

	if got := recordsNamed(ring, "service.upstream_error"); len(got) != 0 {
		t.Errorf("service.upstream_error records = %d, want 0 for a client error", len(got))
	}
}
