package providers

import (
	"altune/go-api/internal/feedback/service"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// throttlingGitHub is a fake GitHub API whose first create call answers with the
// given (rate-limit/abuse) response and every later call succeeds with a 201.
// It counts every request that actually reaches it.
func throttlingGitHub(t *testing.T, status int, headers map[string]string, body string) (*GitHubIssueTracker, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if hits.Add(1) == 1 {
			for k, v := range headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/o/r/issues/7"}`))
	}))
	t.Cleanup(server.Close)
	return newTestTracker(server.URL), &hits
}

type steppedClock struct{ t time.Time }

func (c *steppedClock) now() time.Time { return c.t }

// generousLimits keeps the local sliding-window caps out of the way so only the
// reaction to GitHub's throttling signal can refuse a submission.
var generousLimits = service.SubmissionLimits{
	PerUser:       1000,
	PerUserWindow: time.Minute,
	Global:        1000,
	GlobalWindow:  time.Minute,
}

type noopMetrics struct{}

func (noopMetrics) TrackerCreateFailed(string) {}
func (noopMetrics) SubmissionRejected(string)  {}
func (noopMetrics) SubmissionCreated()         {}

func submitAsFreshUser(svc *service.SubmitReportService) error {
	_, err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), service.SubmitReportInput{
		Kind:    "bug",
		Message: "three downloaded tracks went grey again",
	})
	return err
}

func assertRefusedLocally(t *testing.T, err error) {
	t.Helper()
	var status interface{ HTTPStatus() int }
	if !errors.As(err, &status) || status.HTTPStatus() != http.StatusTooManyRequests {
		t.Fatalf("submission during GitHub's lockout = %v, want a local 429 refusal", err)
	}
}

// TestAdmission_PausesOnGitHubThrottleSignal reproduces #1116: GitHub answering a
// create with a rate-limit/abuse response used to leave the admission layer
// admitting at its fixed local rate, so every following report hit GitHub again
// during the lockout. Now the first throttle response pauses admissions for as
// long as GitHub asked, refusing locally (429) without calling GitHub, and
// admissions resume once that wait has elapsed.
func TestAdmission_PausesOnGitHubThrottleSignal(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers func() map[string]string
		body    string
		// stillPaused is a point inside the requested wait; resumed is past it.
		stillPaused time.Duration
		resumed     time.Duration
	}{
		{
			name:        "429 with Retry-After",
			status:      http.StatusTooManyRequests,
			headers:     func() map[string]string { return map[string]string{"Retry-After": "120"} },
			body:        `{"message":"API rate limit exceeded"}`,
			stillPaused: 119 * time.Second,
			resumed:     120 * time.Second,
		},
		{
			name:        "403 secondary limit with Retry-After",
			status:      http.StatusForbidden,
			headers:     func() map[string]string { return map[string]string{"Retry-After": "300"} },
			body:        `{"message":"You have exceeded a secondary rate limit."}`,
			stillPaused: 299 * time.Second,
			resumed:     300 * time.Second,
		},
		{
			name:   "403 primary limit exhausted until X-RateLimit-Reset",
			status: http.StatusForbidden,
			headers: func() map[string]string {
				return map[string]string{
					"X-RateLimit-Remaining": "0",
					"X-RateLimit-Reset":     strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10),
				}
			},
			body:        `{"message":"API rate limit exceeded"}`,
			stillPaused: 9 * time.Minute,
			resumed:     10*time.Minute + time.Second,
		},
		{
			name:        "403 secondary limit without headers waits at least a minute",
			status:      http.StatusForbidden,
			headers:     func() map[string]string { return nil },
			body:        `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
			stillPaused: 59 * time.Second,
			resumed:     time.Minute,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker, hits := throttlingGitHub(t, tc.status, tc.headers(), tc.body)
			start := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
			clock := &steppedClock{t: start}
			svc := service.NewSubmitReportServiceWithLimits(tracker, noopMetrics{}, generousLimits, clock.now)

			if err := submitAsFreshUser(svc); err == nil {
				t.Fatal("the throttled create should surface as a failure")
			}
			for i := 0; i < 5; i++ {
				assertRefusedLocally(t, submitAsFreshUser(svc))
			}
			clock.t = start.Add(tc.stillPaused)
			assertRefusedLocally(t, submitAsFreshUser(svc))
			if got := hits.Load(); got != 1 {
				t.Fatalf("GitHub was called %d times during its lockout, want 1", got)
			}

			clock.t = start.Add(tc.resumed)
			if err := submitAsFreshUser(svc); err != nil {
				t.Fatalf("submission after GitHub's wait elapsed was refused: %v", err)
			}
			if got := hits.Load(); got != 2 {
				t.Fatalf("GitHub was called %d times, want 2 once the wait elapsed", got)
			}
		})
	}
}

func TestRequestedBackoff_ReadsGitHubHeaders(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cases := []struct {
		name    string
		headers map[string]string
		want    time.Duration
	}{
		{"retry-after seconds", map[string]string{"Retry-After": "42"}, 42 * time.Second},
		{"retry-after wins over reset", map[string]string{"Retry-After": "5", "X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1800000600"}, 5 * time.Second},
		{"reset when exhausted", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1800000600"}, 10 * time.Minute},
		{"reset ignored while quota remains", map[string]string{"X-RateLimit-Remaining": "12", "X-RateLimit-Reset": "1800000600"}, 0},
		{"reset in the past", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1799999000"}, 0},
		{"absurd retry-after does not overflow", map[string]string{"Retry-After": "92233720369"}, 24 * time.Hour},
		{"far-future reset is clamped", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "9223372036854775807"}, 24 * time.Hour},
		{"far-past reset does not wrap", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "-9223372036854775808"}, 0},
		{"negative retry-after falls through", map[string]string{"Retry-After": "-5"}, 0},
		{"garbage values", map[string]string{"Retry-After": "soon", "X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "later"}, 0},
		{"no headers", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.headers {
				h.Set(k, v)
			}
			if got := requestedBackoff(h, now); got != tc.want {
				t.Fatalf("requestedBackoff = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAdmission_NonThrottleFailureDoesNotPause pins that only a rate-limit
// signal pauses admissions: a plain GitHub outage or a dead token keeps admitting.
func TestAdmission_NonThrottleFailureDoesNotPause(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"bad gateway", http.StatusBadGateway, `{"message":"boom"}`},
		{"forbidden without rate-limit signal", http.StatusForbidden, `{"message":"Resource not accessible by personal access token"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker, hits := throttlingGitHub(t, tc.status, nil, tc.body)
			clock := &steppedClock{t: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)}
			svc := service.NewSubmitReportServiceWithLimits(tracker, noopMetrics{}, generousLimits, clock.now)

			_ = submitAsFreshUser(svc)
			if err := submitAsFreshUser(svc); err != nil {
				t.Fatalf("a non-throttle failure paused admissions: %v", err)
			}
			if got := hits.Load(); got != 2 {
				t.Fatalf("GitHub was called %d times, want 2", got)
			}
		})
	}
}
