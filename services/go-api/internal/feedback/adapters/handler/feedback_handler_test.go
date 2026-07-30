package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/feedback/service"
	"altune/go-api/internal/shared"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var testUserId = shared.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))

var verifyAsTestUser = auth.VerifierFunc(func(context.Context, string) (shared.UserId, error) {
	return testUserId, nil
})

type stubTracker struct {
	last *domain.Report
	err  error
}

func (s *stubTracker) Create(_ context.Context, report *domain.Report) (ports.IssueRef, error) {
	if s.err != nil {
		return ports.IssueRef{}, s.err
	}
	s.last = report
	return ports.IssueRef{Number: 42, URL: "https://github.com/o/r/issues/42"}, nil
}

func router(tracker ports.IssueTracker, limit int) chi.Router {
	handler := NewFeedbackHandler(
		service.NewSubmitReportService(tracker, service.NewRateLimiter(limit, time.Hour)),
	)
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

func TestSubmitReport_Returns201WithIssueRef(t *testing.T) {
	tracker := &stubTracker{}
	rec := post(t, router(tracker, 5), validBody())

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
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
	rec := send(t, router(&stubTracker{}, 5), buf, false)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSubmitReport_Returns400OnMalformedBody(t *testing.T) {
	rec := send(t, router(&stubTracker{}, 5), bytes.NewBufferString("{not json"), true)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSubmitReport_Returns400OnUnknownKind(t *testing.T) {
	body := validBody()
	body["kind"] = "rant"
	rec := post(t, router(&stubTracker{}, 5), body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmitReport_Returns429WithRetryAfter(t *testing.T) {
	r := router(&stubTracker{}, 1)
	if rec := post(t, r, validBody()); rec.Code != http.StatusCreated {
		t.Fatalf("first report: %d", rec.Code)
	}

	rec := post(t, r, validBody())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected a Retry-After header")
	}
}

func TestSubmitReport_Returns500WhenTheTrackerFails(t *testing.T) {
	rec := post(t, router(&stubTracker{err: errors.New("github is down")}, 5), validBody())

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
