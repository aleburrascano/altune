package service

import (
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func retryAfterHeader(t *testing.T, err error) string {
	t.Helper()
	rec := httptest.NewRecorder()
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/", nil), err)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	return rec.Header().Get("Retry-After")
}

func TestAdmission_LimitsCarryRetryAfter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limits := SubmissionLimits{PerUser: 1, PerUserWindow: time.Minute, Global: 2, GlobalWindow: 2 * time.Minute}
	a := newSubmissionAdmission(limits, func() time.Time { return now })

	if _, err := a.admit("u"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Second)
	if got := retryAfterHeader(t, admitErr(a, "u")); got != "40" {
		t.Fatalf("user limit Retry-After = %q, want 40", got)
	}
	if _, err := a.admit("v"); err != nil {
		t.Fatal(err)
	}
	if got := retryAfterHeader(t, admitErr(a, "w")); got != "100" {
		t.Fatalf("global limit Retry-After = %q, want 100", got)
	}

	a.observe(t.Context(), throttleAfter(90*time.Second))
	now = now.Add(time.Second)
	if got := retryAfterHeader(t, admitErr(a, "x")); got != "89" {
		t.Fatalf("paused Retry-After = %q, want 89", got)
	}
}

type throttleAfter time.Duration

func (t throttleAfter) Error() string                    { return "throttled" }
func (t throttleAfter) Throttled() (time.Duration, bool) { return time.Duration(t), true }
