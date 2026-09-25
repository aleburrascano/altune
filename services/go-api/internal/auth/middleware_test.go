package auth

import (
	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	"altune/go-api/internal/auth/ports"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func stubVerifier(userID shared.UserId, err error) VerifierFunc {
	return func(context.Context, string) (VerifiedToken, error) {
		return VerifiedToken{UserID: userID, ExpiresAt: time.Now().Add(time.Hour)}, err
	}
}

func noopHandler() (http.Handler, *bool) {
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	return h, &called
}

func decodeRejectBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode reject body: %v", err)
	}
	return body
}

func TestMiddleware_MissingHeader(t *testing.T) {
	next, called := noopHandler()
	handler := Middleware(stubVerifier(shared.UserId{}, nil))(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	body := decodeRejectBody(t, rec)
	if body["reason"] != string(ReasonMissing) {
		t.Errorf("reason: got %q, want %q", body["reason"], ReasonMissing)
	}
	if *called {
		t.Error("next handler should not have been called")
	}
}

func TestMiddleware_MalformedHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{name: "Basic scheme", header: "Basic abc123"},
		{name: "no space separator", header: "Bearertoken"},
		{name: "empty scheme", header: " token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, called := noopHandler()
			handler := Middleware(stubVerifier(shared.UserId{}, nil))(next)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			body := decodeRejectBody(t, rec)
			if body["reason"] != string(ReasonMalformed) {
				t.Errorf("reason: got %q, want %q", body["reason"], ReasonMalformed)
			}
			if *called {
				t.Error("next handler should not have been called")
			}
		})
	}
}

// The bound is the middleware's own, so it holds whatever MaxHeaderBytes the
// server is configured with; httptest applies no header limit at all.
func TestMiddleware_OversizedBearerIsMalformedAndNeverVerified(t *testing.T) {
	for _, size := range []int{maxBearerTokenBytes + 1, 64 << 10, 1 << 20} {
		verified := false
		verifier := VerifierFunc(func(context.Context, string) (VerifiedToken, error) {
			verified = true
			return VerifiedToken{UserID: shared.NewUserId(uuid.New()), ExpiresAt: time.Now().Add(time.Hour)}, nil
		})
		next, called := noopHandler()
		handler := Middleware(verifier)(next)

		rec := serveBearer(handler, "203.0.113.7:1", strings.Repeat("a", size))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%d-byte bearer: status %d, want 401", size, rec.Code)
		}
		if body := decodeRejectBody(t, rec); body["reason"] != string(ReasonMalformed) {
			t.Errorf("%d-byte bearer: reason %q, want %q", size, body["reason"], ReasonMalformed)
		}
		if verified || *called {
			t.Errorf("%d-byte bearer: verifier ran=%v next ran=%v, want neither", size, verified, *called)
		}
	}
}

func TestMiddleware_BearerAtTheBoundStillReachesTheVerifier(t *testing.T) {
	var got string
	verifier := VerifierFunc(func(_ context.Context, token string) (VerifiedToken, error) {
		got = token
		return VerifiedToken{UserID: shared.NewUserId(uuid.New()), ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	next, _ := noopHandler()
	token := strings.Repeat("a", maxBearerTokenBytes)

	rec := serveBearer(Middleware(verifier)(next), "203.0.113.7:1", token)

	if rec.Code != http.StatusOK || got != token {
		t.Fatalf("%d-byte bearer: status %d, verifier got %d bytes; want 200 and the full token", len(token), rec.Code, len(got))
	}
}

// An oversized bearer is rejected by a length check before admission, like any
// other malformed header: it does no verification work, so it spends none of
// the caller's failure budget.
func TestMiddleware_OversizedBearerDoesNotSpendFailureBudget(t *testing.T) {
	handler, _ := throttledMiddleware(stubVerifier(shared.NewUserId(uuid.New()), nil))
	for range testFailureLimits.Burst * 5 {
		serveBearer(handler, "203.0.113.7:1", strings.Repeat("a", maxBearerTokenBytes+1))
	}
	if rec := serveBearer(handler, "203.0.113.7:1", "valid"); rec.Code != http.StatusOK {
		t.Fatalf("valid bearer after oversized ones: status %d, want 200", rec.Code)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	uid := shared.NewUserId(uuid.New())
	verifier := stubVerifier(uid, nil)

	var capturedUID shared.UserId
	var uidFound bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUID, uidFound = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(verifier)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if !uidFound {
		t.Fatal("expected userId in context, got none")
	}
	if capturedUID.UUID() != uid.UUID() {
		t.Errorf("context userId: got %v, want %v", capturedUID.UUID(), uid.UUID())
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	verifier := stubVerifier(shared.UserId{},
		&InvalidTokenError{Reason: ReasonExpired, Detail: "token expired"})
	next, called := noopHandler()
	handler := Middleware(verifier)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	body := decodeRejectBody(t, rec)
	if body["reason"] != string(ReasonExpired) {
		t.Errorf("reason: got %q, want %q", body["reason"], ReasonExpired)
	}
	if *called {
		t.Error("next handler should not have been called")
	}
}

func captureJSONLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// findLogRecord returns the first JSON record whose msg is want.
func findLogRecord(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unparseable log line %q: %v", line, err)
		}
		if rec["msg"] == want {
			return rec
		}
	}
	t.Fatalf("log %q not emitted; got:\n%s", want, buf.String())
	return nil
}

// A rejection that names neither the caller nor the route cannot be traced back
// to the source driving it — auth.throttled already carries both. The bearer
// itself stays out: a 401 log is not a place to put a credential.
func TestMiddleware_TokenRejectedLogNamesTheCallerAndRoute(t *testing.T) {
	const bearer = "gqxz-token-value-that-must-not-be-logged"
	buf := captureJSONLog(t)
	next, _ := noopHandler()
	handler := Middleware(stubVerifier(shared.UserId{}, &InvalidTokenError{Reason: ReasonExpired}))(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/tracks", nil)
	req.RemoteAddr = "203.0.113.42:5555"
	req.Header.Set("Authorization", "Bearer "+bearer)

	handler.ServeHTTP(httptest.NewRecorder(), req)

	rejected := findLogRecord(t, buf, "auth.token_rejected")
	for attr, want := range map[string]string{"client": "203.0.113.42", "path": "/v1/tracks"} {
		if rejected[attr] != want {
			t.Errorf("auth.token_rejected %s = %v, want %q", attr, rejected[attr], want)
		}
	}
	if strings.Contains(buf.String(), bearer) {
		t.Errorf("bearer value reached the log output:\n%s", buf.String())
	}
}

func TestMiddleware_UnreachableVerifierIs503NotTokenRejection(t *testing.T) {
	verifierThatCouldNotRun := stubVerifier(shared.UserId{}, errors.New("fetch JWKS: connection refused"))
	next, called := noopHandler()
	handler := Middleware(verifierThatCouldNotRun)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	body := decodeRejectBody(t, rec)
	if body["reason"] != "" {
		t.Errorf("reason: got %q, want none (infra failure is not a token rejection)", body["reason"])
	}
	if *called {
		t.Error("next handler should not have been called")
	}
}

func TestMiddleware_RepeatedFailedVerificationsFromOneCallerAreThrottled(t *testing.T) {
	calls := 0
	verifier := VerifierFunc(func(context.Context, string) (VerifiedToken, error) {
		calls++
		return VerifiedToken{}, &InvalidTokenError{Reason: ReasonSignatureInvalid}
	})
	next, _ := noopHandler()
	handler := Middleware(verifier)(next)

	throttled := 0
	for range 200 {
		rec := serveBearer(handler, "203.0.113.7:4444", "garbage")
		if rec.Code == http.StatusTooManyRequests {
			throttled++
		}
	}

	if calls >= 200 || throttled == 0 {
		t.Fatalf("200 bogus bearers from one caller: verifier ran %d times, %d throttled; want verification capped", calls, throttled)
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

var testFailureLimits = FailureLimits{Burst: 3, Refill: time.Minute, MaxClients: 100}

func throttledMiddleware(verifier TokenVerifier) (http.Handler, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	next, _ := noopHandler()
	return middleware(verifier, newFailureThrottle(testFailureLimits, clock.now), ports.NoopAuthMetrics())(next), clock
}

func TestMiddleware_ThrottleRefusesWithoutVerifyingOnceFailuresExhaustBurst(t *testing.T) {
	calls := 0
	verifier := VerifierFunc(func(context.Context, string) (VerifiedToken, error) {
		calls++
		return VerifiedToken{}, &InvalidTokenError{Reason: ReasonSignatureInvalid}
	})
	handler, clock := throttledMiddleware(verifier)

	for i := range testFailureLimits.Burst {
		if rec := serveBearer(handler, "203.0.113.7:1", "garbage"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401 while under the burst", i, rec.Code)
		}
	}
	rec := serveBearer(handler, "203.0.113.7:2", "garbage")
	if rec.Code != http.StatusTooManyRequests || calls != testFailureLimits.Burst {
		t.Fatalf("after burst: status %d, verifier calls %d; want 429 and %d calls", rec.Code, calls, testFailureLimits.Burst)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After: got %q, want %q", got, "60")
	}

	clock.advance(testFailureLimits.Refill)
	if rec := serveBearer(handler, "203.0.113.7:3", "garbage"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after one refill: status %d, want 401 (one attempt earned back)", rec.Code)
	}
	if rec := serveBearer(handler, "203.0.113.7:4", "garbage"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after spending the refilled token: status %d, want 429", rec.Code)
	}
}

func TestMiddleware_ThrottleIsPerClient(t *testing.T) {
	handler, _ := throttledMiddleware(stubVerifier(shared.UserId{}, &InvalidTokenError{Reason: ReasonExpired}))
	for range testFailureLimits.Burst + 1 {
		serveBearer(handler, "203.0.113.7:1", "t")
	}
	if rec := serveBearer(handler, "198.51.100.9:1", "t"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("second client: status %d, want 401 (not throttled by the first)", rec.Code)
	}
}

func TestMiddleware_SuccessfulVerificationsAreNeverThrottled(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	// Verification takes real time, so the clock ticks between reserving a
	// failure token and refunding it on success. A refund pinned to that later
	// reading is dropped by rate.CancelAt, which would charge every success and
	// throttle this caller after Burst requests. Refunding at the reservation
	// instant keeps successes free.
	verifier := VerifierFunc(func(context.Context, string) (VerifiedToken, error) {
		clock.advance(time.Millisecond)
		return VerifiedToken{UserID: shared.NewUserId(uuid.New()), ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	next, _ := noopHandler()
	handler := middleware(verifier, newFailureThrottle(testFailureLimits, clock.now), ports.NoopAuthMetrics())(next)

	for i := range testFailureLimits.Burst * 10 {
		if rec := serveBearer(handler, "203.0.113.7:1", "valid"); rec.Code != http.StatusOK {
			t.Fatalf("valid attempt %d: status %d, want 200", i, rec.Code)
		}
	}
}

func TestMiddleware_VerifierUnavailableCountsAsFailedAttempt(t *testing.T) {
	handler, _ := throttledMiddleware(stubVerifier(shared.UserId{}, errors.New("fetch JWKS: connection refused")))
	for range testFailureLimits.Burst {
		serveBearer(handler, "203.0.113.7:1", "t")
	}
	if rec := serveBearer(handler, "203.0.113.7:1", "t"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429: JWKS-outage attempts must not be free", rec.Code)
	}
}

// Expired and garbage tokens must spend the budget identically and the
// throttled response must be byte-identical, so lockout adds no oracle beyond
// the TokenRejectReason already returned before it.
func TestMiddleware_ThrottledResponseRevealsNothingAboutTheToken(t *testing.T) {
	bodies := map[TokenRejectReason]string{}
	for _, reason := range []TokenRejectReason{ReasonExpired, ReasonSignatureInvalid, ReasonMalformed} {
		handler, _ := throttledMiddleware(stubVerifier(shared.UserId{}, &InvalidTokenError{Reason: reason}))
		for range testFailureLimits.Burst {
			serveBearer(handler, "203.0.113.7:1", string(reason))
		}
		rec := serveBearer(handler, "203.0.113.7:1", string(reason))
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("%s: status %d after burst, want 429", reason, rec.Code)
		}
		body := decodeRejectBody(t, rec)
		if body["reason"] != "" {
			t.Errorf("%s: throttled body carries reason %q, want none", reason, body["reason"])
		}
		bodies[reason] = body["detail"]
	}
	if bodies[ReasonExpired] != bodies[ReasonSignatureInvalid] || bodies[ReasonExpired] != bodies[ReasonMalformed] {
		t.Errorf("throttled bodies differ by token kind: %v", bodies)
	}
}

func TestMiddleware_ConcurrentFailuresCannotOvershootBurst(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	verifier := VerifierFunc(func(context.Context, string) (VerifiedToken, error) {
		calls.Add(1)
		<-release
		return VerifiedToken{}, &InvalidTokenError{Reason: ReasonSignatureInvalid}
	})
	handler, _ := throttledMiddleware(verifier)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { serveBearer(handler, "203.0.113.7:1", "garbage") })
	}
	time.AfterFunc(50*time.Millisecond, func() { close(release) })
	wg.Wait()

	if got := int(calls.Load()); got > testFailureLimits.Burst {
		t.Fatalf("50 concurrent bogus bearers ran the verifier %d times, want at most %d", got, testFailureLimits.Burst)
	}
}

func serveBearer(handler http.Handler, remoteAddr, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func verifies(verified VerifiedToken) VerifierFunc {
	return func(context.Context, string) (VerifiedToken, error) {
		return verified, nil
	}
}

func serveThroughMiddleware(t *testing.T, verifier TokenVerifier) (time.Time, bool) {
	t.Helper()
	var expiresAt time.Time
	var known bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		expiresAt, known = TokenExpiryFromContext(r.Context())
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	Middleware(verifier)(next).ServeHTTP(httptest.NewRecorder(), req)
	return expiresAt, known
}

func TestMiddleware_TokenExpiryReachesHandler(t *testing.T) {
	want := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	verifier := verifies(VerifiedToken{UserID: shared.NewUserId(uuid.New()), ExpiresAt: want})

	got, known := serveThroughMiddleware(t, verifier)

	if !known || !got.Equal(want) {
		t.Fatalf("token expiry on context = %v (known %v), want %v", got, known, want)
	}
}

func TestMiddleware_RejectsVerifiedTokenWithoutExpiry(t *testing.T) {
	next, called := noopHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	Middleware(verifies(VerifiedToken{UserID: shared.NewUserId(uuid.New())}))(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a token with no expiry", rec.Code)
	}
	if *called {
		t.Fatal("handler ran for a token with no expiry")
	}
	if reason := decodeRejectBody(t, rec)["reason"]; reason != string(ReasonClaimMissingEXP) {
		t.Fatalf("reason = %q, want %q", reason, ReasonClaimMissingEXP)
	}
}

func TestUntilTokenExpiry_EndsAtExpiryWithCause(t *testing.T) {
	parent := ContextWithTokenExpiry(context.Background(), time.Now().Add(20*time.Millisecond))

	ctx, cancel := UntilTokenExpiry(parent)
	defer cancel()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived the token expiry")
	}
	if cause := context.Cause(ctx); !errors.Is(cause, ErrTokenExpired) {
		t.Fatalf("cause = %v, want ErrTokenExpired", cause)
	}
}

func TestUntilTokenExpiry_UnknownExpiryEndsOnlyWithParent(t *testing.T) {
	ctx, cancel := UntilTokenExpiry(context.Background())
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("context with no token expiry ended on its own")
	case <-time.After(50 * time.Millisecond):
	}
}

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
