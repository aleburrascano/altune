package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/feedback/service"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var testUserId = shared.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))

var verifyAsTestUser = auth.VerifierFunc(func(context.Context, string) (shared.UserId, error) {
	return testUserId, nil
})

type noopMetrics struct{}

func (noopMetrics) TrackerCreateFailed(string) {}
func (noopMetrics) SubmissionRejected(string)  {}
func (noopMetrics) SubmissionCreated()         {}

type stubTracker struct {
	last    *domain.Report
	creates int
	err     error
}

func (s *stubTracker) Create(_ context.Context, report *domain.Report) (ports.IssueRef, error) {
	if s.err != nil {
		return ports.IssueRef{}, s.err
	}
	s.last = report
	s.creates++
	return ports.IssueRef{Number: 42, URL: "https://github.com/o/r/issues/42"}, nil
}

func router(tracker ports.IssueTracker) chi.Router {
	handler := NewFeedbackHandler(service.NewSubmitReportService(tracker, noopMetrics{}))
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(auth.Middleware(verifyAsTestUser))
		gr.Mount("/feedback", handler.Routes())
	})
	return r
}

func post(t *testing.T, r chi.Router, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(body); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return send(t, r, buf, true)
}

func send(t *testing.T, r chi.Router, body io.Reader, authorized bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/feedback/reports", body)
	req.Header.Set("Content-Type", "application/json")
	if authorized {
		req.Header.Set("Authorization", "Bearer fake-token")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func validBody() map[string]string {
	return map[string]string{
		"kind":        "bug",
		"message":     "three downloaded tracks went grey again",
		"app_version": "1.4.0",
		"platform":    "ios",
		"os_version":  "18.2",
		"screen":      "settings",
	}
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
}

func TestSubmitReport_Returns201WithIssueRef(t *testing.T) {
	tracker := &stubTracker{}
	rec := post(t, router(tracker), validBody())

	assertStatus(t, rec, http.StatusCreated)
	var resp SubmitReportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.IssueNumber != 42 {
		t.Fatalf("issue_number = %d, want 42", resp.IssueNumber)
	}
	if tracker.last.Diagnostics.Screen != "settings" {
		t.Fatalf("diagnostics did not reach the tracker: %+v", tracker.last.Diagnostics)
	}
}

func TestSubmitReport_Requires401WithoutAuth(t *testing.T) {
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(validBody())
	rec := send(t, router(&stubTracker{}), buf, false)

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestSubmitReport_Returns400OnMalformedBody(t *testing.T) {
	rec := send(t, router(&stubTracker{}), bytes.NewBufferString("{not json"), true)

	assertStatus(t, rec, http.StatusBadRequest)
}

func TestSubmitReport_Returns400OnUnknownKind(t *testing.T) {
	body := validBody()
	body["kind"] = "rant"
	rec := post(t, router(&stubTracker{}), body)

	assertStatus(t, rec, http.StatusBadRequest)
}

func TestSubmitReport_Returns400OnInvisibleOnlyMessage(t *testing.T) {
	tracker := &stubTracker{}
	body := validBody()
	body["message"] = strings.Repeat(string(rune(0x200B)), domain.MinMessageRunes)
	rec := post(t, router(tracker), body)

	assertStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "feedback.validation_error") {
		t.Fatalf("400 body missing the error code: %s", rec.Body.String())
	}
	if tracker.last != nil {
		t.Fatal("an invisible-only report still reached the tracker")
	}
}

func TestSubmitReport_DoesNotEchoOversizedKind(t *testing.T) {
	body := validBody()
	body["kind"] = strings.Repeat("\x01", 10000)
	rec := post(t, router(&stubTracker{}), body)

	assertStatus(t, rec, http.StatusRequestEntityTooLarge)
	if !strings.Contains(rec.Body.String(), "request.too_large") {
		t.Fatalf("413 body missing the error code: %s", rec.Body.String())
	}
	if rec.Body.Len() > 512 {
		t.Fatalf("413 body is %d bytes, want the oversized kind not echoed back", rec.Body.Len())
	}
}

func TestSubmitReport_AcceptsBackToBackReportsUpToTheLimit(t *testing.T) {
	r := router(&stubTracker{})

	for i := 0; i < service.DefaultSubmissionLimits.PerUser; i++ {
		assertStatus(t, post(t, r, validBody()), http.StatusCreated)
	}
}

func TestSubmitReport_Returns429OnceAUserExceedsTheLimit(t *testing.T) {
	tracker := &stubTracker{}
	r := router(tracker)
	for i := 0; i < service.DefaultSubmissionLimits.PerUser; i++ {
		assertStatus(t, post(t, r, validBody()), http.StatusCreated)
	}
	tracker.last = nil

	rec := post(t, r, validBody())

	assertStatus(t, rec, http.StatusTooManyRequests)
	if !strings.Contains(rec.Body.String(), "feedback.rate_limited") {
		t.Fatalf("429 body missing the error code: %s", rec.Body.String())
	}
	if tracker.last != nil {
		t.Fatal("a throttled report still reached the tracker")
	}
}

func TestSubmitReport_Returns500WhenTheTrackerFails(t *testing.T) {
	rec := post(t, router(&stubTracker{err: errors.New("github is down")}), validBody())

	assertStatus(t, rec, http.StatusInternalServerError)
}

// TestSubmitReport_IdempotencyKeyHeaderCollapsesRetries proves the header is
// plumbed through: two identical POSTs sharing an Idempotency-Key create only
// one issue and both return the first result.
func TestSubmitReport_IdempotencyKeyHeaderCollapsesRetries(t *testing.T) {
	tracker := &stubTracker{}
	r := router(tracker)

	first := postWithKey(t, r, validBody(), "retry-abc")
	assertStatus(t, first, http.StatusCreated)
	second := postWithKey(t, r, validBody(), "retry-abc")
	assertStatus(t, second, http.StatusCreated)

	if tracker.creates != 1 {
		t.Fatalf("shared idempotency key created %d issues, want 1", tracker.creates)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("replay body %q differs from first %q", second.Body.String(), first.Body.String())
	}
}

func postWithKey(t *testing.T, r chi.Router, body any, key string) *httptest.ResponseRecorder {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(body); err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/feedback/reports", buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fake-token")
	req.Header.Set("Idempotency-Key", key)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
