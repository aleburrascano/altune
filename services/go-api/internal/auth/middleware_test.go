package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func stubVerifier(userID shared.UserId, err error) VerifierFunc {
	return func(context.Context, string) (shared.UserId, error) {
		return userID, err
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
	verifier := VerifierFunc(func(context.Context, string) (shared.UserId, error) {
		calls++
		return shared.UserId{}, &InvalidTokenError{Reason: ReasonSignatureInvalid}
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
	return middleware(verifier, newFailureThrottle(testFailureLimits, clock.now))(next), clock
}

func TestMiddleware_ThrottleRefusesWithoutVerifyingOnceFailuresExhaustBurst(t *testing.T) {
	calls := 0
	verifier := VerifierFunc(func(context.Context, string) (shared.UserId, error) {
		calls++
		return shared.UserId{}, &InvalidTokenError{Reason: ReasonSignatureInvalid}
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
	verifier := VerifierFunc(func(context.Context, string) (shared.UserId, error) {
		clock.advance(time.Millisecond)
		return shared.NewUserId(uuid.New()), nil
	})
	next, _ := noopHandler()
	handler := middleware(verifier, newFailureThrottle(testFailureLimits, clock.now))(next)

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
	verifier := VerifierFunc(func(context.Context, string) (shared.UserId, error) {
		calls.Add(1)
		<-release
		return shared.UserId{}, &InvalidTokenError{Reason: ReasonSignatureInvalid}
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

func TestUserIDFromContext(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		uid := shared.NewUserId(uuid.New())
		ctx := context.WithValue(context.Background(), userIDKey, uid)

		got, ok := UserIDFromContext(ctx)
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if got.UUID() != uid.UUID() {
			t.Errorf("userId: got %v, want %v", got.UUID(), uid.UUID())
		}
	})

	t.Run("absent", func(t *testing.T) {
		got, ok := UserIDFromContext(context.Background())
		if ok {
			t.Fatal("expected ok=false, got true")
		}
		if !got.IsZero() {
			t.Errorf("expected zero UserId, got %v", got)
		}
	})
}

func TestRequireUserID(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		uid := shared.NewUserId(uuid.New())
		ctx := context.WithValue(context.Background(), userIDKey, uid)
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		got, ok := RequireUserID(rec, req)
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if got.UUID() != uid.UUID() {
			t.Errorf("userId: got %v, want %v", got.UUID(), uid.UUID())
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status: got %d, want %d (no error written)", rec.Code, http.StatusOK)
		}
	})

	t.Run("absent writes 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		got, ok := RequireUserID(rec, req)
		if ok {
			t.Fatal("expected ok=false, got true")
		}
		if !got.IsZero() {
			t.Errorf("expected zero UserId, got %v", got)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}
