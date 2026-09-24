package providers

import (
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func retryAfterFor(resp *http.Response) (int, string) {
	rec := httptest.NewRecorder()
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodPost, "/", nil), statusError(resp, time.Now()))
	return rec.Code, rec.Header().Get("Retry-After")
}

func TestTrackerError_ForwardsBoundedRetryAfter(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{name: "github hint is forwarded", header: "30", want: "30"},
		{name: "an absurd hint is bounded", header: "86400", want: "3600"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": {tc.header}},
				Body:       http.NoBody,
			}
			code, got := retryAfterFor(resp)
			if code != http.StatusServiceUnavailable || got != tc.want {
				t.Fatalf("status %d Retry-After %q, want 503 and %q", code, got, tc.want)
			}
		})
	}
}

func TestTrackerError_NoHintSendsNoRetryAfter(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}, Body: http.NoBody}
	if _, got := retryAfterFor(resp); got != "" {
		t.Fatalf("Retry-After = %q, want none", got)
	}
}
