package httpserver_test

import (
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRetryAfterSeconds_RoundsUpToAtLeastOne(t *testing.T) {
	for wait, want := range map[time.Duration]string{
		0:                       "1",
		time.Nanosecond:         "1",
		500 * time.Millisecond:  "1",
		time.Second:             "1",
		1200 * time.Millisecond: "2",
		30 * time.Second:        "30",
	} {
		if got := httpserver.RetryAfterSeconds(wait); got != want {
			t.Errorf("RetryAfterSeconds(%v) = %q, want %q", wait, got, want)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	rec := httptest.NewRecorder()
	httpserver.WriteJSON(rec, http.StatusCreated, payload{Name: "altune"})

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

func captureLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logging.Setup("debug")
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

func recordsAtLevel(ring *logging.RingBuffer, level slog.Level) []logging.CapturedRecord {
	var at []logging.CapturedRecord
	for _, captured := range ring.Snapshot() {
		if captured.Level == level.String() {
			at = append(at, captured)
		}
	}
	return at
}

func onlyRecord(t *testing.T, ring *logging.RingBuffer, message string) logging.CapturedRecord {
	t.Helper()
	named := recordsNamed(ring, message)
	if len(named) != 1 {
		t.Fatalf("%s records = %d, want 1", message, len(named))
	}
	return named[0]
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) httpserver.Problem {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	var p httpserver.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem %q: %v", rec.Body.String(), err)
	}
	return p
}

func requestWithCorrelationID(id string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/favorites", nil)
	return r.WithContext(logging.ContextWithCorrelationID(r.Context(), id))
}

func TestHandleServiceError_AnswersACodedErrorWithAProblemCarryingTheCorrelationID(t *testing.T) {
	rec := httptest.NewRecorder()
	err := fmt.Errorf("add favorite for user 7: %w", apierror.NewError(apierror.CodeDiscoveryFavoritesFull, "favorites are full: at most 50 per account"))

	httpserver.HandleServiceError(rec, requestWithCorrelationID("ab12cd34"), err)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	want := `{"type":"urn:altune:problem:discovery.favorites_full","title":"Favorites are full","status":409,` +
		`"detail":"favorites are full: at most 50 per account","instance":"ab12cd34","code":"discovery.favorites_full"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
}

func TestHandleServiceError_KeepsTheDetailAndCodeTodaysClientReads(t *testing.T) {
	rec := httptest.NewRecorder()

	httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), apierror.NewError(apierror.CodeCatalogTrackNotFound, "track not found"))

	var legacy struct {
		Detail string `json:"detail"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("decode into the {detail, code} shape: %v", err)
	}
	if legacy.Detail != "track not found" || legacy.Code != "catalog.track_not_found" {
		t.Errorf("legacy view = %+v, want detail and code unchanged", legacy)
	}
}

func TestHandleServiceError_AnswersEveryCatalogCodeWithTheStatusOfItsKind(t *testing.T) {
	statusOfKind := map[apierror.Kind]int{
		apierror.KindInternal:        500,
		apierror.KindInvalid:         400,
		apierror.KindUnauthenticated: 401,
		apierror.KindForbidden:       403,
		apierror.KindNotFound:        404,
		apierror.KindConflict:        409,
		apierror.KindTooLarge:        413,
		apierror.KindRateLimited:     429,
		apierror.KindUpstreamFailed:  502,
		apierror.KindUnavailable:     503,
		apierror.KindTimeout:         504,
	}
	for _, code := range apierror.Catalog() {
		want, known := statusOfKind[code.Kind()]
		if !known {
			t.Errorf("code %s has kind %d, which this test does not know", code, code.Kind())
			continue
		}
		rec := httptest.NewRecorder()

		httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), apierror.NewError(code, ""))

		p := decodeProblem(t, rec)
		if rec.Code != want || p.Status != want || p.Code != code.String() || p.Title != code.Title() || p.Detail != code.Title() {
			t.Errorf("%s answered %d with %+v, want %d carrying its code and title", code, rec.Code, p, want)
		}
	}
}

func TestHandleServiceError_HidesAnUntypedErrorBehindInternal(t *testing.T) {
	rec := httptest.NewRecorder()

	httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), errors.New("pq: password authentication failed for user altune"))

	p := decodeProblem(t, rec)
	if rec.Code != http.StatusInternalServerError || p.Code != "internal" || p.Detail != "internal server error" {
		t.Errorf("answered %d %+v, want 500 internal with the generic detail", rec.Code, p)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("body leaks the cause: %s", rec.Body.String())
	}
}

func TestHandleServiceError_ClientThatLeftGetsNoBodyAndNoErrorLine(t *testing.T) {
	ring := captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/library", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	httpserver.HandleServiceError(rec, r, fmt.Errorf("list tracks: %w", context.Canceled))

	if rec.Code != 499 || rec.Body.Len() != 0 {
		t.Errorf("answered %d with %q, want 499 and no body", rec.Code, rec.Body.String())
	}
	logged := onlyRecord(t, ring, "request.cancelled")
	if logged.Level != slog.LevelInfo.String() || logged.Attrs["path"] != "/v1/library" {
		t.Errorf("request.cancelled = %s %v, want INFO with the path", logged.Level, logged.Attrs)
	}
	if errs := recordsAtLevel(ring, slog.LevelError); len(errs) != 0 {
		t.Errorf("ERROR records = %+v, want none for a client that left", errs)
	}
}

func TestHandleServiceError_RequestPastItsDeadlineGets504Timeout(t *testing.T) {
	ring := captureLogs(t)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/search", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	httpserver.HandleServiceError(rec, r, fmt.Errorf("query: %w", context.DeadlineExceeded))

	p := decodeProblem(t, rec)
	if rec.Code != http.StatusGatewayTimeout || p.Code != "request.timeout" {
		t.Errorf("answered %d %+v, want 504 request.timeout", rec.Code, p)
	}
	if logged := onlyRecord(t, ring, "request.timed_out"); logged.Level != slog.LevelWarn.String() {
		t.Errorf("request.timed_out level = %s, want WARN", logged.Level)
	}
	if errs := recordsAtLevel(ring, slog.LevelError); len(errs) != 0 {
		t.Errorf("ERROR records = %+v, want none for our own deadline", errs)
	}
}

func TestHandleServiceError_CodedErrorWinsOverAnExpiredDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	httpserver.HandleServiceError(rec, r, apierror.NewError(apierror.CodeCatalogTemporarilyDown, ""))

	if p := decodeProblem(t, rec); rec.Code != http.StatusServiceUnavailable || p.Code != "catalog.temporarily_unavailable" {
		t.Errorf("answered %d %+v, want the coded 503", rec.Code, p)
	}
}

func TestHandleServiceError_ARefusalStaysARefusalAfterTheClientLeft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	httpserver.HandleServiceError(rec, r, apierror.NewError(apierror.CodeObservePrincipalRequired, ""))

	if p := decodeProblem(t, rec); rec.Code != http.StatusForbidden || p.Code != "observe.principal_required" {
		t.Errorf("answered %d %+v, want the coded 403: only a cancellation error means the client left", rec.Code, p)
	}
}

func TestHandleServiceError_InternalCancellationOrTimeoutIsAFaultWhileTheRequestLives(t *testing.T) {
	for name, cause := range map[string]error{"canceled": context.Canceled, "deadline": context.DeadlineExceeded} {
		t.Run(name, func(t *testing.T) {
			ring := captureLogs(t)
			rec := httptest.NewRecorder()

			httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), fmt.Errorf("provider call: %w", cause))

			if p := decodeProblem(t, rec); rec.Code != http.StatusInternalServerError || p.Code != "internal" {
				t.Errorf("answered %d %+v, want 500 internal", rec.Code, p)
			}
			if logged := onlyRecord(t, ring, "request.failed"); logged.Level != slog.LevelError.String() {
				t.Errorf("request.failed level = %s, want ERROR", logged.Level)
			}
		})
	}
}

func TestHandleServiceError_LogsAServerFaultOnceAtErrorWithItsCause(t *testing.T) {
	ring := captureLogs(t)
	cause := errors.New("dial tcp 10.0.0.5:5432: connection refused")
	err := fmt.Errorf("load track 42: %w", cause)

	httpserver.HandleServiceError(httptest.NewRecorder(), requestWithCorrelationID("ff00ff00"), err)

	errs := recordsAtLevel(ring, slog.LevelError)
	if len(errs) != 1 || errs[0].Message != "request.failed" {
		t.Fatalf("ERROR records = %+v, want exactly one request.failed", errs)
	}
	want := map[string]string{"method": "POST", "path": "/v1/favorites", "status": "500", "code": "internal", "corr_id": "ff00ff00"}
	for key, value := range want {
		if errs[0].Attrs[key] != value {
			t.Errorf("attr %s = %q, want %q", key, errs[0].Attrs[key], value)
		}
	}
	if !strings.Contains(errs[0].Attrs["error"], cause.Error()) {
		t.Errorf("error attr = %q, want it to carry the cause", errs[0].Attrs["error"])
	}
}

func TestHandleServiceError_LogsAClientFaultAtInfoOnly(t *testing.T) {
	ring := captureLogs(t)

	httpserver.HandleServiceError(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/favorites", nil), apierror.NewError(apierror.CodeDiscoveryFavoritesFull, ""))

	logged := onlyRecord(t, ring, "request.rejected")
	if logged.Level != slog.LevelInfo.String() || logged.Attrs["status"] != "409" || logged.Attrs["code"] != "discovery.favorites_full" {
		t.Errorf("request.rejected = %s %v, want INFO with status 409 and the code", logged.Level, logged.Attrs)
	}
	if len(ring.Snapshot()) != 1 {
		t.Errorf("records = %+v, want only the info line", ring.Snapshot())
	}
}

func TestHandleServiceError_SetsRetryAfterFromTheError(t *testing.T) {
	rec := httptest.NewRecorder()

	httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), apierror.NewError(apierror.CodePlaybackUnavailable, "").WithRetryAfter(1500*time.Millisecond))

	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q, want 2", got)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestHandleServiceError_KeepsARetryAfterTheCallerAlreadySet(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Retry-After", "30")

	httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), apierror.NewError(apierror.CodeDiscoveryRateLimited, "").WithRetryAfter(time.Second))

	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want the caller's 30", got)
	}
}

func TestHandleServiceError_SetsNoRetryAfterWithoutAWait(t *testing.T) {
	rec := httptest.NewRecorder()

	httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), apierror.NewError(apierror.CodeDiscoveryRateLimited, ""))

	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Errorf("Retry-After = %q, want none", got)
	}
}

func TestHandleServiceError_AnErrorCarryingItsOwnStatusIsStillInternal(t *testing.T) {
	rec := httptest.NewRecorder()
	err := fmt.Errorf("create issue: %w", &selfDescribedTestError{cause: errors.New("dial tcp api.github.com: connection refused")})

	httpserver.HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/v1/feedback", nil), err)

	p := decodeProblem(t, rec)
	if rec.Code != http.StatusInternalServerError || p.Code != "internal" || p.Detail != "internal server error" {
		t.Errorf("answered %d %+v, want 500 internal: only a shared/domain code sets the answer", rec.Code, p)
	}
}

type selfDescribedTestError struct{ cause error }

func (e *selfDescribedTestError) Error() string        { return e.cause.Error() }
func (e *selfDescribedTestError) Unwrap() error        { return e.cause }
func (e *selfDescribedTestError) HTTPStatus() int      { return http.StatusBadGateway }
func (e *selfDescribedTestError) ErrorCode() string    { return "tracker_unreachable" }
func (e *selfDescribedTestError) ClientDetail() string { return "upstream unavailable" }

func TestWriteJSON_RefusesAValueItCannotEncodeBeforeWritingAnything(t *testing.T) {
	rec := httptest.NewRecorder()
	defer func() {
		if recover() == nil {
			t.Error("WriteJSON of a channel did not panic")
		}
		if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
			t.Errorf("WriteJSON wrote %q before failing, want nothing", rec.Body.String())
		}
	}()

	httpserver.WriteJSON(rec, http.StatusOK, map[string]any{"c": make(chan int)})
}
