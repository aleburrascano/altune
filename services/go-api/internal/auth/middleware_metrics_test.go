package auth

import (
	"altune/go-api/internal/auth/ports"
	"altune/go-api/internal/shared"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	authmetrics "altune/go-api/internal/auth/adapters/metrics"

	"github.com/google/uuid"
)

// recordingMetrics is an AuthMetrics that remembers every call.
type recordingMetrics struct {
	mu          sync.Mutex
	rejected    []string
	throttled   int
	unavailable int
}

var _ ports.AuthMetrics = (*recordingMetrics)(nil)

func (m *recordingMetrics) TokenRejected(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejected = append(m.rejected, reason)
}

func (m *recordingMetrics) RequestThrottled() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.throttled++
}

func (m *recordingMetrics) VerifierUnavailable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unavailable++
}

func (*recordingMetrics) JWKSFetchFailed() {}

func TestMiddleware_CountsEveryRejectionAndOutage(t *testing.T) {
	unavailableErr := errors.New("fetch JWKS: connection refused")
	tests := []struct {
		name            string
		header          string
		verifyErr       error
		wantStatus      int
		wantRejected    []string
		wantUnavailable int
	}{
		{"missing header", "", nil, http.StatusUnauthorized, []string{string(ReasonMissing)}, 0},
		{"malformed header", "Basic abc", nil, http.StatusUnauthorized, []string{string(ReasonMalformed)}, 0},
		{"invalid token", "Bearer t", &InvalidTokenError{Reason: ReasonSignatureInvalid}, http.StatusUnauthorized, []string{string(ReasonSignatureInvalid)}, 0},
		{"verifier unavailable", "Bearer t", unavailableErr, http.StatusServiceUnavailable, nil, 1},
		{"valid token", "Bearer t", nil, http.StatusOK, nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := &recordingMetrics{}
			next, _ := noopHandler()
			handler := Middleware(stubVerifier(shared.NewUserId(uuid.New()), tt.verifyErr), WithMetrics(metrics))(next)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tt.wantStatus)
			}
			if !equalStrings(metrics.rejected, tt.wantRejected) {
				t.Errorf("TokenRejected calls %v, want %v", metrics.rejected, tt.wantRejected)
			}
			if metrics.unavailable != tt.wantUnavailable {
				t.Errorf("VerifierUnavailable calls %d, want %d", metrics.unavailable, tt.wantUnavailable)
			}
		})
	}
}

// A throttled 429 never ran the verifier, so it is neither a token rejection
// nor an outage and must not inflate either counter.
func TestMiddleware_ThrottledRequestIsNotCounted(t *testing.T) {
	metrics := &recordingMetrics{}
	clock := &fakeClock{}
	next, _ := noopHandler()
	verifier := stubVerifier(shared.UserId{}, &InvalidTokenError{Reason: ReasonExpired})
	handler := middleware(verifier, newFailureThrottle(testFailureLimits, clock.now), metrics)(next)

	for range testFailureLimits.Burst {
		serveBearer(handler, "203.0.113.7:1", "t")
	}
	if rec := serveBearer(handler, "203.0.113.7:1", "t"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d after burst, want 429", rec.Code)
	}
	if got := len(metrics.rejected); got != testFailureLimits.Burst {
		t.Errorf("TokenRejected calls %d, want %d (the throttled request must not count)", got, testFailureLimits.Burst)
	}
}

// A throttled caller never reaches the verifier, so the 429 count is the only
// number left that shows it, and it has to move on every refusal rather than
// only on the one that opened the lockout.
func TestMiddleware_EveryThrottledRequestIsCounted(t *testing.T) {
	metrics := &recordingMetrics{}
	clock := &fakeClock{}
	next, _ := noopHandler()
	verifier := stubVerifier(shared.UserId{}, &InvalidTokenError{Reason: ReasonExpired})
	handler := middleware(verifier, newFailureThrottle(testFailureLimits, clock.now), metrics)(next)

	for range testFailureLimits.Burst {
		serveBearer(handler, "203.0.113.10:1", "t")
	}
	const refusals = 3
	for i := range refusals {
		if rec := serveBearer(handler, "203.0.113.10:1", "t"); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("refusal %d: status %d, want 429", i, rec.Code)
		}
	}

	if metrics.throttled != refusals {
		t.Errorf("RequestThrottled calls %d, want %d", metrics.throttled, refusals)
	}
}

// The wired expvar adapter moves the published counters that
// GET /admin/metrics/live exposes.
func TestMiddleware_ExpvarCountersIncrementThroughRealMiddleware(t *testing.T) {
	metrics := WithMetrics(authmetrics.NewExpvarAuthMetrics())
	next, _ := noopHandler()
	rejecting := Middleware(stubVerifier(shared.UserId{}, &InvalidTokenError{Reason: ReasonExpired}), metrics)(next)
	unavailable := Middleware(stubVerifier(shared.UserId{}, errors.New("fetch JWKS: timeout")), metrics)(next)

	before := authmetrics.ReadSnapshot()
	serveBearer(rejecting, "203.0.113.8:1", "t")
	serveBearer(unavailable, "203.0.113.9:1", "t")
	after := authmetrics.ReadSnapshot()

	if after.TokenRejections != before.TokenRejections+1 {
		t.Errorf("token_rejections_total %d, want %d", after.TokenRejections, before.TokenRejections+1)
	}
	if want := before.TokenRejectionsByReason[string(ReasonExpired)] + 1; after.TokenRejectionsByReason[string(ReasonExpired)] != want {
		t.Errorf("token_rejections_by_reason_total[expired] %d, want %d", after.TokenRejectionsByReason[string(ReasonExpired)], want)
	}
	if after.VerifierUnavailable != before.VerifierUnavailable+1 {
		t.Errorf("verifier_unavailable_total %d, want %d", after.VerifierUnavailable, before.VerifierUnavailable+1)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
