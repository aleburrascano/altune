package httputil

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestTimeout_CancelsRequestContextAfterBudget(t *testing.T) {
	const budget = 50 * time.Millisecond
	var cause error
	h := RequestTimeout(budget)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			cause = r.Context().Err()
		case <-time.After(cutoffWait):
		}
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !errors.Is(cause, context.DeadlineExceeded) {
		t.Fatalf("context error = %v, want deadline exceeded", cause)
	}
}
