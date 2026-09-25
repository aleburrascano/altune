package httputil

import (
	"altune/go-api/internal/shared/logging"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type codedTestError struct {
	msg    string
	status int
	code   string
}

func (e *codedTestError) Error() string     { return e.msg }
func (e *codedTestError) HTTPStatus() int   { return e.status }
func (e *codedTestError) ErrorCode() string { return e.code }

type statusOnlyTestError struct {
	msg    string
	status int
}

func (e *statusOnlyTestError) Error() string   { return e.msg }
func (e *statusOnlyTestError) HTTPStatus() int { return e.status }

func TestHandleServiceError_GoldenBodies(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"explicit code", &codedTestError{"track not found", 404, "catalog.track_not_found"}, http.StatusNotFound, `{"detail":"track not found","code":"catalog.track_not_found"}`},
		{"validation explicit code", &codedTestError{"title is required", 400, "catalog.validation_error"}, http.StatusBadRequest, `{"detail":"title is required","code":"catalog.validation_error"}`},
		{"status fallback not_found", &statusOnlyTestError{"missing", 404}, http.StatusNotFound, `{"detail":"missing","code":"not_found"}`},
		{"status fallback bad_request", &statusOnlyTestError{"bad", 400}, http.StatusBadRequest, `{"detail":"bad","code":"bad_request"}`},
		{"status fallback conflict", &statusOnlyTestError{"dupe", 409}, http.StatusConflict, `{"detail":"dupe","code":"conflict"}`},
		{"status fallback other", &statusOnlyTestError{"teapot", 418}, http.StatusTeapot, `{"detail":"teapot","code":"error"}`},
		{"empty explicit code falls back", &codedTestError{"missing", 404, ""}, http.StatusNotFound, `{"detail":"missing","code":"not_found"}`},
		{"generic non-status error", errors.New("boom"), http.StatusInternalServerError, `{"detail":"internal server error","code":"internal"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), tt.err)

			if rec.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tt.wantBody {
				t.Errorf("body: got %s, want %s", got, tt.wantBody)
			}
		})
	}
}

func TestHandleServiceError_DetailStaysByteIdentical(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantDetail string
	}{
		{"explicit code", &codedTestError{"track not found", 404, "catalog.track_not_found"}, "track not found"},
		{"status fallback", &statusOnlyTestError{"bad input", 400}, "bad input"},
		{"generic 500", errors.New("boom"), "internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), tt.err)

			var legacy struct {
				Detail string `json:"detail"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
				t.Fatalf("decode into legacy shape: %v", err)
			}
			if legacy.Detail != tt.wantDetail {
				t.Errorf("detail: got %q, want %q", legacy.Detail, tt.wantDetail)
			}
		})
	}
}

func TestHandleServiceError_CodeIsBranchable(t *testing.T) {
	rec := httptest.NewRecorder()
	HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil),
		&codedTestError{"track not found", 404, "catalog.track_not_found"})

	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var branch string
	switch body.Code {
	case "catalog.track_not_found":
		branch = "track-missing"
	case "catalog.validation_error":
		branch = "invalid"
	default:
		branch = "other"
	}
	if branch != "track-missing" {
		t.Errorf("branch: got %q, want %q (code %q)", branch, "track-missing", body.Code)
	}
}

func TestHandleServiceError_CodeNeverEmpty(t *testing.T) {
	errs := []error{
		&codedTestError{"x", 400, "catalog.validation_error"},
		&codedTestError{"x", 404, ""},
		&statusOnlyTestError{"x", 400},
		&statusOnlyTestError{"x", 404},
		&statusOnlyTestError{"x", 409},
		&statusOnlyTestError{"x", 418},
		errors.New("boom"),
	}
	for _, e := range errs {
		rec := httptest.NewRecorder()
		HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), e)
		var body ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Code == "" {
			t.Errorf("code was empty for %T (%v)", e, e)
		}
	}
}

func TestDirectHelpersOmitCode(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusServiceUnavailable, "authentication unavailable")

	got := strings.TrimSpace(rec.Body.String())
	if got != `{"detail":"authentication unavailable"}` {
		t.Errorf("body: got %s, want %s", got, `{"detail":"authentication unavailable"}`)
	}
	if strings.Contains(got, "code") {
		t.Errorf("direct helper leaked a code field: %s", got)
	}
}

func TestWriteJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, payload{Name: "altune"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusCreated)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}

	var got payload
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Name != "altune" {
		t.Errorf("body.name: got %q, want %q", got.Name, "altune")
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusTeapot, "I'm a teapot")

	if rec.Code != http.StatusTeapot {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusTeapot)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Detail != "I'm a teapot" {
		t.Errorf("detail: got %q, want %q", body.Detail, "I'm a teapot")
	}
}

func TestNotFound(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantDetail string
	}{
		{
			name:       "custom message",
			message:    "track not found",
			wantDetail: "track not found",
		},
		{
			name:       "empty message uses default",
			message:    "",
			wantDetail: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NotFound(rec, tt.message)

			if rec.Code != http.StatusNotFound {
				t.Errorf("status: got %d, want %d", rec.Code, http.StatusNotFound)
			}

			var body ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Detail != tt.wantDetail {
				t.Errorf("detail: got %q, want %q", body.Detail, tt.wantDetail)
			}
		})
	}
}

func TestBadRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	BadRequest(rec, "invalid input")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Detail != "invalid input" {
		t.Errorf("detail: got %q, want %q", body.Detail, "invalid input")
	}
}

func TestBadRequestCode(t *testing.T) {
	rec := httptest.NewRecorder()
	BadRequestCode(rec, "discovery.invalid_param", "offset must be an integer")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	want := `{"detail":"offset must be an integer","code":"discovery.invalid_param"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body: got %s, want %s", got, want)
	}
}

func TestInternalError(t *testing.T) {
	rec := httptest.NewRecorder()
	InternalError(rec)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Detail != "internal server error" {
		t.Errorf("detail: got %q, want %q", body.Detail, "internal server error")
	}
}

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
