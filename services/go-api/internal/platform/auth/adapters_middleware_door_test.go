package auth_test

import (
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const (
	maxBearerBytes        = 8 << 10
	reasonMissing         = "missing"
	reasonMalformed       = "malformed"
	reasonExpired         = "expired"
	reasonClaimMissingEXP = "claim_missing_exp"
)

func stubVerifier(userID ids.UserId, err error) auth.VerifierFunc {
	return func(context.Context, string) (auth.VerifiedToken, error) {
		return auth.VerifiedToken{UserID: userID, ExpiresAt: time.Now().Add(time.Hour)}, err
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

func decodeRejectBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode reject body: %v", err)
	}
	return body
}

func TestMiddleware_MissingHeader(t *testing.T) {
	next, called := noopHandler()
	handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, nil), time.Now)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	body := decodeRejectBody(t, rec)
	if body["code"] != "auth.token_"+reasonMissing {
		t.Errorf("code: got %q, want auth.token_%s", body["code"], reasonMissing)
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
			handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, nil), time.Now)(next)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			body := decodeRejectBody(t, rec)
			if body["code"] != "auth.token_"+reasonMalformed {
				t.Errorf("code: got %q, want auth.token_%s", body["code"], reasonMalformed)
			}
			if *called {
				t.Error("next handler should not have been called")
			}
		})
	}
}

func TestMiddleware_OversizedBearerIsMalformedAndNeverVerified(t *testing.T) {
	for _, size := range []int{maxBearerBytes + 1, 64 << 10, 1 << 20} {
		verified := false
		verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
			verified = true
			return auth.VerifiedToken{UserID: ids.NewUserId(uuid.New()), ExpiresAt: time.Now().Add(time.Hour)}, nil
		})
		next, called := noopHandler()
		handler := auth.MustMiddleware(verifier, time.Now)(next)

		rec := serveBearer(handler, "203.0.113.7:1", strings.Repeat("a", size))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%d-byte bearer: status %d, want 401", size, rec.Code)
		}
		if body := decodeRejectBody(t, rec); body["code"] != "auth.token_"+reasonMalformed {
			t.Errorf("%d-byte bearer: code %q, want auth.token_%s", size, body["code"], reasonMalformed)
		}
		if verified || *called {
			t.Errorf("%d-byte bearer: verifier ran=%v next ran=%v, want neither", size, verified, *called)
		}
	}
}

func TestMiddleware_BearerAtTheBoundStillReachesTheVerifier(t *testing.T) {
	var got string
	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		got = token
		return auth.VerifiedToken{UserID: ids.NewUserId(uuid.New()), ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	next, _ := noopHandler()
	token := strings.Repeat("a", maxBearerBytes)

	rec := serveBearer(auth.MustMiddleware(verifier, time.Now)(next), "203.0.113.7:1", token)

	if rec.Code != http.StatusOK || got != token {
		t.Fatalf("%d-byte bearer: status %d, verifier got %d bytes; want 200 and the full token", len(token), rec.Code, len(got))
	}
}

func TestMiddleware_OversizedBearerDoesNotSpendFailureBudget(t *testing.T) {
	handler, _ := throttledMiddleware(stubVerifier(ids.NewUserId(uuid.New()), nil))
	for range testFailureLimits.Burst * 5 {
		serveBearer(handler, "203.0.113.7:1", strings.Repeat("a", maxBearerBytes+1))
	}
	if rec := serveBearer(handler, "203.0.113.7:1", "valid"); rec.Code != http.StatusOK {
		t.Fatalf("valid bearer after oversized ones: status %d, want 200", rec.Code)
	}
}

func TestMiddleware_TheVerifiedUserReachesTheRequestCompleteLine(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	ring := logging.Setup("info")
	uid := ids.NewUserId(uuid.New())
	next, _ := noopHandler()

	serveBearer(httpserver.RequestLogger(auth.MustMiddleware(stubVerifier(uid, nil), time.Now)(next)), "203.0.113.7:1", "valid")

	var complete []logging.CapturedRecord
	for _, rec := range ring.Snapshot() {
		if rec.Message == "request.complete" {
			complete = append(complete, rec)
		}
	}
	if len(complete) != 1 || complete[0].Attrs["user_id"] != uid.String() {
		t.Fatalf("request.complete records = %+v, want one carrying user_id %s", complete, uid)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	uid := ids.NewUserId(uuid.New())
	verifier := stubVerifier(uid, nil)

	var capturedUID ids.UserId
	var uidFound bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUID, uidFound = requestuser.UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := auth.MustMiddleware(verifier, time.Now)(next)

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
	verifier := stubVerifier(ids.UserId{},
		&auth.InvalidTokenError{Reason: reasonExpired, Detail: "token expired"})
	next, called := noopHandler()
	handler := auth.MustMiddleware(verifier, time.Now)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	body := decodeRejectBody(t, rec)
	if body["code"] != "auth.token_"+reasonExpired {
		t.Errorf("code: got %q, want auth.token_%s", body["code"], reasonExpired)
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

func TestMiddleware_TokenRejectedLogNamesTheCallerAndRoute(t *testing.T) {
	const bearer = "gqxz-token-value-that-must-not-be-logged"
	buf := captureJSONLog(t)
	next, _ := noopHandler()
	handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, &auth.InvalidTokenError{Reason: reasonExpired}), time.Now)(next)
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
	verifierThatCouldNotRun := stubVerifier(ids.UserId{}, errors.New("fetch JWKS: connection refused"))
	next, called := noopHandler()
	handler := auth.MustMiddleware(verifierThatCouldNotRun, time.Now)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	body := decodeRejectBody(t, rec)
	if body["code"] != "auth.unavailable" {
		t.Errorf("code: got %q, want auth.unavailable (infra failure is not a token rejection)", body["code"])
	}
	if *called {
		t.Error("next handler should not have been called")
	}
}

func TestMiddleware_RepeatedFailedVerificationsFromOneCallerAreThrottled(t *testing.T) {
	calls := 0
	verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		calls++
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
	})
	next, _ := noopHandler()
	handler := auth.MustMiddleware(verifier, time.Now)(next)

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

var testFailureLimits = auth.FailureLimits{Burst: 3, Refill: time.Minute, MaxClients: 100}

func throttledMiddleware(verifier auth.TokenVerifier) (http.Handler, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	next, _ := noopHandler()
	return auth.MustMiddleware(verifier, clock.now, auth.WithFailureLimits(testFailureLimits))(next), clock
}

func TestMiddleware_ThrottleRefusesWithoutVerifyingOnceFailuresExhaustBurst(t *testing.T) {
	calls := 0
	verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		calls++
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
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
	handler, _ := throttledMiddleware(stubVerifier(ids.UserId{}, &auth.InvalidTokenError{Reason: reasonExpired}))
	for range testFailureLimits.Burst + 1 {
		serveBearer(handler, "203.0.113.7:1", "t")
	}
	if rec := serveBearer(handler, "198.51.100.9:1", "t"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("second client: status %d, want 401 (not throttled by the first)", rec.Code)
	}
}

func TestMiddleware_SuccessfulVerificationsAreNeverThrottled(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		clock.advance(time.Millisecond)
		return auth.VerifiedToken{UserID: ids.NewUserId(uuid.New()), ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	next, _ := noopHandler()
	handler := auth.MustMiddleware(verifier, clock.now, auth.WithFailureLimits(testFailureLimits))(next)

	for i := range testFailureLimits.Burst * 10 {
		if rec := serveBearer(handler, "203.0.113.7:1", "valid"); rec.Code != http.StatusOK {
			t.Fatalf("valid attempt %d: status %d, want 200", i, rec.Code)
		}
	}
}

func TestMiddleware_VerifierUnavailableCountsAsFailedAttempt(t *testing.T) {
	handler, _ := throttledMiddleware(stubVerifier(ids.UserId{}, errors.New("fetch JWKS: connection refused")))
	for range testFailureLimits.Burst {
		serveBearer(handler, "203.0.113.7:1", "t")
	}
	if rec := serveBearer(handler, "203.0.113.7:1", "t"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429: JWKS-outage attempts must not be free", rec.Code)
	}
}

func TestMiddleware_AnOutageStaysAnOutageWhenTheClientAlsoLeft(t *testing.T) {
	metrics := &recordingMetrics{}
	next, _ := noopHandler()
	ctx, cancel := context.WithCancel(context.Background())
	verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		cancel()
		return auth.VerifiedToken{}, errors.New("fetch JWKS: dial tcp 10.0.0.9:443: connection refused")
	})
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()

	auth.MustMiddleware(verifier, time.Now, auth.WithMetrics(metrics))(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503: the verifier failed, the client leaving does not change that", rec.Code)
	}
	if metrics.unavailable != 1 {
		t.Errorf("VerifierUnavailable calls %d, want 1: an outage is counted even when the client gave up", metrics.unavailable)
	}
}

func TestMiddleware_ClientCancelledDuringVerifyIsNotAnOutage(t *testing.T) {
	logs := captureJSONLog(t)
	metrics := &recordingMetrics{}
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	cancelled := stubVerifier(ids.UserId{}, fmt.Errorf("fetch JWKS: %w", context.Canceled))
	next, _ := noopHandler()
	handler := auth.MustMiddleware(cancelled, clock.now, auth.WithFailureLimits(testFailureLimits), auth.WithMetrics(metrics))(next)

	serve := func() *httptest.ResponseRecorder {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		req.RemoteAddr = "203.0.113.7:1"
		req.Header.Set("Authorization", "Bearer t")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(); rec.Code != 499 || rec.Body.Len() != 0 {
		t.Fatalf("answered %d %q, want 499 and no body for a client that left", rec.Code, rec.Body.String())
	}
	if metrics.unavailable != 0 {
		t.Errorf("VerifierUnavailable calls %d, want 0", metrics.unavailable)
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("client cancel logged at ERROR:\n%s", logs.String())
	}
	for range testFailureLimits.Burst - 1 {
		serve()
	}
	if rec := serve(); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status %d after burst, want 429: cancelled attempts still charge the throttle", rec.Code)
	}
}

func TestMiddleware_ThrottledResponseRevealsNothingAboutTheToken(t *testing.T) {
	bodies := map[string]string{}
	for _, rejection := range []*auth.InvalidTokenError{{Reason: reasonExpired}, {Reason: auth.ReasonSignatureInvalid}, {Reason: reasonMalformed}} {
		reason := string(rejection.Reason)
		handler, _ := throttledMiddleware(stubVerifier(ids.UserId{}, rejection))
		for range testFailureLimits.Burst {
			serveBearer(handler, "203.0.113.7:1", reason)
		}
		rec := serveBearer(handler, "203.0.113.7:1", reason)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("%s: status %d after burst, want 429", reason, rec.Code)
		}
		body := decodeRejectBody(t, rec)
		if body["code"] != "auth.throttled" {
			t.Errorf("%s: throttled body carries code %q, want auth.throttled", reason, body["code"])
		}
		bodies[reason] = fmt.Sprint(body["detail"])
	}
	if bodies[reasonExpired] != bodies[string(auth.ReasonSignatureInvalid)] || bodies[reasonExpired] != bodies[reasonMalformed] {
		t.Errorf("throttled bodies differ by token kind: %v", bodies)
	}
}

func TestMiddleware_ConcurrentFailuresCannotOvershootBurst(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		calls.Add(1)
		<-release
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
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

func verifies(verified auth.VerifiedToken) auth.VerifierFunc {
	return func(context.Context, string) (auth.VerifiedToken, error) {
		return verified, nil
	}
}

func serveThroughMiddleware(t *testing.T, verifier auth.TokenVerifier) (time.Time, bool) {
	t.Helper()
	var expiresAt time.Time
	var known bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		expiresAt, known = requestuser.TokenExpiryFromContext(r.Context())
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	auth.MustMiddleware(verifier, time.Now)(next).ServeHTTP(httptest.NewRecorder(), req)
	return expiresAt, known
}

func TestMiddleware_TokenExpiryReachesHandler(t *testing.T) {
	want := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	verifier := verifies(auth.VerifiedToken{UserID: ids.NewUserId(uuid.New()), ExpiresAt: want})

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

	auth.MustMiddleware(verifies(auth.VerifiedToken{UserID: ids.NewUserId(uuid.New())}), time.Now)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a token with no expiry", rec.Code)
	}
	if *called {
		t.Fatal("handler ran for a token with no expiry")
	}
	if code := decodeRejectBody(t, rec)["code"]; code != "auth.token_"+reasonClaimMissingEXP {
		t.Fatalf("code = %q, want auth.token_%s", code, reasonClaimMissingEXP)
	}
}

type recordingMetrics struct {
	mu          sync.Mutex
	rejected    []string
	throttled   int
	unavailable int
}

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
		{"missing header", "", nil, http.StatusUnauthorized, []string{reasonMissing}, 0},
		{"malformed header", "Basic abc", nil, http.StatusUnauthorized, []string{reasonMalformed}, 0},
		{"invalid token", "Bearer t", &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}, http.StatusUnauthorized, []string{string(auth.ReasonSignatureInvalid)}, 0},
		{"verifier unavailable", "Bearer t", unavailableErr, http.StatusServiceUnavailable, nil, 1},
		{"failed key refresh after signature rejection", "Bearer t", errors.Join(errors.New("refresh JWKS after signature rejection"), unavailableErr), http.StatusServiceUnavailable, nil, 1},
		{"valid token", "Bearer t", nil, http.StatusOK, nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := &recordingMetrics{}
			next, _ := noopHandler()
			handler := auth.MustMiddleware(stubVerifier(ids.NewUserId(uuid.New()), tt.verifyErr), time.Now, auth.WithMetrics(metrics))(next)

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

func TestMiddleware_ThrottledRequestIsNotCounted(t *testing.T) {
	metrics := &recordingMetrics{}
	clock := &fakeClock{}
	next, _ := noopHandler()
	verifier := stubVerifier(ids.UserId{}, &auth.InvalidTokenError{Reason: reasonExpired})
	handler := auth.MustMiddleware(verifier, clock.now, auth.WithFailureLimits(testFailureLimits), auth.WithMetrics(metrics))(next)

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

func TestMiddleware_EveryThrottledRequestIsCounted(t *testing.T) {
	metrics := &recordingMetrics{}
	clock := &fakeClock{}
	next, _ := noopHandler()
	verifier := stubVerifier(ids.UserId{}, &auth.InvalidTokenError{Reason: reasonExpired})
	handler := auth.MustMiddleware(verifier, clock.now, auth.WithFailureLimits(testFailureLimits), auth.WithMetrics(metrics))(next)

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

func TestMiddleware_ExpvarCountersIncrementThroughRealMiddleware(t *testing.T) {
	metrics := auth.WithMetrics(auth.NewExpvarAuthMetrics())
	next, _ := noopHandler()
	rejecting := auth.MustMiddleware(stubVerifier(ids.UserId{}, &auth.InvalidTokenError{Reason: reasonExpired}), time.Now, metrics)(next)
	unavailable := auth.MustMiddleware(stubVerifier(ids.UserId{}, errors.New("fetch JWKS: timeout")), time.Now, metrics)(next)

	before := auth.ReadSnapshot()
	serveBearer(rejecting, "203.0.113.8:1", "t")
	serveBearer(unavailable, "203.0.113.9:1", "t")
	after := auth.ReadSnapshot()

	if after.TokenRejections != before.TokenRejections+1 {
		t.Errorf("token_rejections_total %d, want %d", after.TokenRejections, before.TokenRejections+1)
	}
	if want := before.TokenRejectionsByReason[reasonExpired] + 1; after.TokenRejectionsByReason[reasonExpired] != want {
		t.Errorf("token_rejections_by_reason_total[expired] %d, want %d", after.TokenRejectionsByReason[reasonExpired], want)
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

func requireExactResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, wwwAuthenticate, body string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status: got %d, want %d", rec.Code, status)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != wwwAuthenticate {
		t.Errorf("WWW-Authenticate: got %q, want %q", got, wwwAuthenticate)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type: got %q, want %q", got, "application/problem+json")
	}
	if got := rec.Body.String(); got != body {
		t.Errorf("body: got %q, want %q", got, body)
	}
}

func TestMiddleware_MissingHeaderBodyIsPinned(t *testing.T) {
	next, called := noopHandler()
	handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, nil), time.Now)(next)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	requireExactResponse(t, rec, http.StatusUnauthorized, "Bearer", `{"type":"urn:altune:problem:auth.token_missing","title":"Authentication required","status":401,"detail":"missing authorization header","code":"auth.token_missing"}`+"\n")
	if *called {
		t.Error("next handler should not have been called")
	}
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", rec.Body.String(), err)
	}
	return body
}

func TestRequireUserID_UnauthorizedMatchesTheMissingTokenRejection(t *testing.T) {
	next, _ := noopHandler()
	viaMiddleware := httptest.NewRecorder()
	auth.MustMiddleware(stubVerifier(ids.UserId{}, nil), time.Now)(next).ServeHTTP(viaMiddleware, httptest.NewRequest(http.MethodGet, "/", nil))
	viaRequire := httptest.NewRecorder()
	requestuser.RequireUserID(viaRequire, httptest.NewRequest(http.MethodGet, "/", nil))

	if viaRequire.Code != viaMiddleware.Code {
		t.Errorf("status: RequireUserID %d, middleware %d", viaRequire.Code, viaMiddleware.Code)
	}
	for _, header := range []string{"WWW-Authenticate", "Content-Type"} {
		if got, want := viaRequire.Header().Get(header), viaMiddleware.Header().Get(header); got != want {
			t.Errorf("%s: RequireUserID %q, middleware %q", header, got, want)
		}
	}
	requireBody, middlewareBody := decodeBody(t, viaRequire), decodeBody(t, viaMiddleware)
	if len(requireBody) != len(middlewareBody) {
		t.Errorf("body keys: RequireUserID %v, middleware %v", requireBody, middlewareBody)
	}
	for key := range middlewareBody {
		if _, ok := requireBody[key]; !ok {
			t.Errorf("RequireUserID body has no %q: %v", key, requireBody)
		}
	}
	if got, want := requireBody["code"], "auth.token_"+reasonMissing; got != want {
		t.Errorf("code: RequireUserID %q, want %q", got, want)
	}
}

func TestMiddleware_LookAlikeSchemesArePinnedAsMalformed(t *testing.T) {
	for _, header := range []string{"Basic abc", "Bearer", "Bearerabc"} {
		t.Run(header, func(t *testing.T) {
			verified := false
			verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
				verified = true
				return auth.VerifiedToken{}, nil
			})
			next, called := noopHandler()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", header)
			rec := httptest.NewRecorder()

			auth.MustMiddleware(verifier, time.Now)(next).ServeHTTP(rec, req)

			requireExactResponse(t, rec, http.StatusUnauthorized, "Bearer", `{"type":"urn:altune:problem:auth.token_malformed","title":"Malformed authorization header","status":401,"detail":"malformed authorization header","code":"auth.token_malformed"}`+"\n")
			if verified || *called {
				t.Errorf("verifier ran=%v next ran=%v, want neither", verified, *called)
			}
		})
	}
}

func TestMiddleware_LowerCaseSchemeReachesTheVerifier(t *testing.T) {
	var got string
	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		got = token
		return auth.VerifiedToken{UserID: ids.NewUserId(uuid.New()), ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	next, called := noopHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "bearer abc")
	rec := httptest.NewRecorder()

	auth.MustMiddleware(verifier, time.Now)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || got != "abc" || !*called {
		t.Fatalf("status %d, verifier token %q, next ran %v; want 200, %q, true", rec.Code, got, *called, "abc")
	}
}

func TestMiddleware_InvalidTokenBodyIsPinned(t *testing.T) {
	next, called := noopHandler()
	handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, &auth.InvalidTokenError{Reason: reasonExpired}), time.Now)(next)

	rec := serveBearer(handler, "203.0.113.7:1", "expired-token")

	requireExactResponse(t, rec, http.StatusUnauthorized, "Bearer", `{"type":"urn:altune:problem:auth.token_expired","title":"Token expired","status":401,"detail":"invalid token","code":"auth.token_expired"}`+"\n")
	if *called {
		t.Error("next handler should not have been called")
	}
}

func TestMiddleware_VerifierUnavailableBodyIsPinned(t *testing.T) {
	next, called := noopHandler()
	handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, errors.New("fetch JWKS: connection refused")), time.Now)(next)

	rec := serveBearer(handler, "203.0.113.7:1", "some-token")

	requireExactResponse(t, rec, http.StatusServiceUnavailable, "", `{"type":"urn:altune:problem:auth.unavailable","title":"Authentication unavailable","status":503,"detail":"authentication unavailable","code":"auth.unavailable"}`+"\n")
	if *called {
		t.Error("next handler should not have been called")
	}
}

func TestMiddleware_ThrottledBodyIsPinned(t *testing.T) {
	handler, _ := throttledMiddleware(stubVerifier(ids.UserId{}, &auth.InvalidTokenError{Reason: reasonExpired}))
	for range testFailureLimits.Burst {
		serveBearer(handler, "203.0.113.7:1", "t")
	}

	rec := serveBearer(handler, "203.0.113.7:1", "t")

	requireExactResponse(t, rec, http.StatusTooManyRequests, "", `{"type":"urn:altune:problem:auth.throttled","title":"Too many failed authentication attempts","status":429,"detail":"too many failed authentication attempts","code":"auth.throttled"}`+"\n")
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After: got %q, want %q", got, "60")
	}
}

func TestMiddleware_ThrottledRetryAfterIsNeverZero(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	next, _ := noopHandler()
	limits := auth.FailureLimits{Burst: 1, Refill: time.Millisecond, MaxClients: 10}
	handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, &auth.InvalidTokenError{Reason: reasonExpired}), clock.now, auth.WithFailureLimits(limits))(next)
	serveBearer(handler, "203.0.113.7:1", "t")

	rec := serveBearer(handler, "203.0.113.7:1", "t")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429 inside a sub-millisecond refill", rec.Code)
	}

	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After: got %q, want %q", got, "1")
	}
}

func TestMiddleware_AClientThatLeftDuringVerificationGets499WithNoBody(t *testing.T) {
	next, _ := noopHandler()
	ctx, cancel := context.WithCancel(context.Background())
	verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		cancel()
		return auth.VerifiedToken{}, context.Canceled
	})
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()

	auth.MustMiddleware(verifier, time.Now)(next).ServeHTTP(rec, req)

	if rec.Code != 499 || rec.Body.Len() != 0 {
		t.Errorf("answered %d with %q, want 499 and no body", rec.Code, rec.Body.String())
	}
}

func TestMiddleware_EachRejectionReasonAnswersItsOwnCode(t *testing.T) {
	rejections := []*auth.InvalidTokenError{
		{Reason: "signature_invalid"},
		{Reason: "expired"},
		{Reason: "revoked"},
		{Reason: "claim_missing_exp"},
		{Reason: "claim_invalid_iss"},
		{Reason: "claim_invalid_aud"},
		{Reason: "claim_invalid_sub"},
		{Reason: "claim_invalid_iat"},
	}
	for _, rejection := range rejections {
		reason := string(rejection.Reason)
		next, _ := noopHandler()
		handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, rejection), time.Now)(next)

		rec := serveBearer(handler, "203.0.113.7:1", "t")

		body := decodeRejectBody(t, rec)
		if rec.Code != http.StatusUnauthorized || body["code"] != "auth.token_"+reason || body["status"] != float64(http.StatusUnauthorized) {
			t.Errorf("%s: answered %d %v, want 401 auth.token_%s", reason, rec.Code, body, reason)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
			t.Errorf("%s: Content-Type %q, want application/problem+json", reason, got)
		}
	}
}

func TestMiddleware_AnUnknownRejectionReasonAnswersTheGenericInvalidTokenCode(t *testing.T) {
	next, _ := noopHandler()
	handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, &auth.InvalidTokenError{Reason: "from_a_future_verifier"}), time.Now)(next)

	rec := serveBearer(handler, "203.0.113.7:1", "t")

	if body := decodeRejectBody(t, rec); rec.Code != http.StatusUnauthorized || body["code"] != "auth.token_invalid" {
		t.Errorf("answered %d %v, want 401 auth.token_invalid", rec.Code, body)
	}
}

func TestMiddleware_AVerifierOutageIsLoggedOnceAtErrorWithItsCause(t *testing.T) {
	logs := captureJSONLog(t)
	next, _ := noopHandler()
	handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, errors.New("fetch JWKS: connection refused")), time.Now)(next)

	serveBearer(handler, "203.0.113.7:1", "t")

	errorLines := strings.Count(logs.String(), `"level":"ERROR"`)
	if errorLines != 1 {
		t.Fatalf("ERROR lines = %d, want 1:\n%s", errorLines, logs.String())
	}
	record := findLogRecord(t, logs, "request.failed")
	if record["code"] != "auth.unavailable" || !strings.Contains(fmt.Sprint(record["error"]), "connection refused") {
		t.Errorf("request.failed = %v, want code auth.unavailable carrying the cause", record)
	}
}

func clientAddr(i int) string {
	return fmt.Sprintf("198.18.%d.%d:4444", i/250, i%250+1)
}

func failingVerifier() auth.VerifierFunc {
	return func(context.Context, string) (auth.VerifiedToken, error) {
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
	}
}

func limitedMiddleware(limits auth.FailureLimits) (http.Handler, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	next, _ := noopHandler()
	return auth.MustMiddleware(failingVerifier(), clock.now, auth.WithFailureLimits(limits))(next), clock
}

func admitted(handler http.Handler, addr string) bool {
	return serveBearer(handler, addr, "garbage").Code != http.StatusTooManyRequests
}

func TestMiddleware_RefusesANilClock(t *testing.T) {
	mw, err := auth.Middleware(failingVerifier(), nil)

	if !errors.Is(err, auth.ErrMiddlewareClockRequired) || mw != nil {
		t.Fatalf("Middleware(nil clock) = (%v, %v), want ErrMiddlewareClockRequired and no middleware", mw != nil, err)
	}
}

func TestFailureThrottle_NonPositiveLimitsKeepTheDefaults(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	cases := []struct {
		name   string
		limits auth.FailureLimits
		now    func() time.Time
		burst  int
	}{
		{"all zero", auth.FailureLimits{}, clock.now, 20},
		{"zero refill", auth.FailureLimits{Burst: 3, MaxClients: 10}, clock.now, 3},
		{"negative refill", auth.FailureLimits{Burst: 3, Refill: -time.Second, MaxClients: 10}, clock.now, 3},
		{"zero max clients", auth.FailureLimits{Burst: 3, Refill: time.Hour}, clock.now, 3},
		{"negative burst", auth.FailureLimits{Burst: -1, Refill: time.Hour, MaxClients: 10}, clock.now, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, _ := noopHandler()
			handler := auth.MustMiddleware(failingVerifier(), tc.now, auth.WithFailureLimits(tc.limits))(next)
			codes := map[int]int{}
			for i := range tc.burst + 5 {
				admitted(handler, clientAddr(i))
				codes[serveBearer(handler, "203.0.113.7:1", "garbage").Code]++
			}
			if codes[http.StatusUnauthorized] != tc.burst || codes[http.StatusTooManyRequests] != 5 {
				t.Fatalf("%d failed attempts from one client: %v, want %d x 401 then 5 x 429", tc.burst+5, codes, tc.burst)
			}
		})
	}
}

func TestFailureThrottle_TrackedClientsStayBounded(t *testing.T) {
	handler, _ := limitedMiddleware(auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 8})

	for i := range 1000 {
		if !admitted(handler, clientAddr(i)) {
			t.Fatalf("fresh client %d refused", i)
		}
	}

	first := admitted(handler, clientAddr(0))
	second := admitted(handler, clientAddr(0))
	if !first || !second {
		t.Fatal("the first client still holds its spent token after 999 newer clients: tracked clients grew past the bound")
	}
}

func TestFailureThrottle_EvictionDropsToTheLowWaterMark(t *testing.T) {
	limits := auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 100}
	handler, _ := limitedMiddleware(limits)
	const keepers, spenders = 90, 10
	for i := range keepers {
		admitted(handler, clientAddr(i))
		admitted(handler, clientAddr(i))
	}
	for i := keepers; i < keepers+spenders; i++ {
		admitted(handler, clientAddr(i))
	}

	admitted(handler, "203.0.113.200:1")

	forgotten := 0
	for i := keepers; i < keepers+spenders; i++ {
		admitted(handler, clientAddr(i))
		if admitted(handler, clientAddr(i)) {
			forgotten++
		}
	}
	if forgotten != spenders {
		t.Fatalf("after one overflow, %d of the %d least-penalised clients were forgotten, want all %d (eviction down to low water, not one at a time)", forgotten, spenders, spenders)
	}
}

func TestFailureThrottle_RefilledClientsArePrunedBeforeEvictingPenalisedOnes(t *testing.T) {
	handler, clock := limitedMiddleware(auth.FailureLimits{Burst: 1, Refill: time.Minute, MaxClients: 2})

	admitted(handler, "203.0.113.1:1")
	clock.advance(time.Minute)
	admitted(handler, "203.0.113.2:1")
	admitted(handler, "203.0.113.3:1")

	if admitted(handler, "203.0.113.2:1") {
		t.Fatal("penalised client was evicted in favour of pruning a refilled one")
	}
}

func TestFailureThrottle_ExhaustedClientSurvivesEvictionChurn(t *testing.T) {
	limits := auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 50}
	handler, _ := limitedMiddleware(limits)
	for i := range limits.MaxClients - 1 {
		admitted(handler, clientAddr(i))
	}
	const attacker = "203.0.113.66:1"
	admitted(handler, attacker)
	admitted(handler, attacker)
	if admitted(handler, attacker) {
		t.Fatal("attacker not exhausted before churn")
	}

	for i := range 500 {
		admitted(handler, clientAddr(1000+i))
	}

	if admitted(handler, attacker) {
		t.Fatal("exhausted client got a fresh burst after eviction churn")
	}
}

func TestFailureThrottle_PruningRefilledClientsSparesAPartlySpentOneFromLaterEviction(t *testing.T) {
	handler, clock := limitedMiddleware(auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 100})
	for i := range 15 {
		admitted(handler, clientAddr(i))
	}
	clock.advance(2 * time.Hour)
	const partlySpent = "203.0.113.9:1"
	admitted(handler, partlySpent)
	clock.advance(30 * time.Minute)
	for i := range 84 {
		admitted(handler, clientAddr(100+i))
	}
	for i := range 11 {
		admitted(handler, clientAddr(200+i))
	}

	if !admitted(handler, partlySpent) || admitted(handler, partlySpent) {
		t.Fatal("the partly spent client got a fresh burst: it was evicted while 15 refilled clients could have been pruned")
	}
}

func TestFailureThrottle_NoClientIsForgottenBelowCapacity(t *testing.T) {
	handler, clock := limitedMiddleware(auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 100})
	const early = "203.0.113.10:1"
	admitted(handler, early)
	clock.advance(30 * time.Minute)
	for i := range 94 {
		admitted(handler, clientAddr(i))
	}

	if !admitted(handler, early) || admitted(handler, early) {
		t.Fatal("with 95 of 100 slots in use, the early client lost its spent token: a client was evicted below capacity")
	}
}

func churnAddr(i int) string {
	return fmt.Sprintf("10.%d.%d.%d:4444", i>>16&255, i>>8&255, i&255)
}

func BenchmarkFailureThrottle_BenignChurn(b *testing.B) { benchmarkChurn(b, 3*time.Second) }

func BenchmarkFailureThrottle_AttackChurn(b *testing.B) { benchmarkChurn(b, 0) }

func benchmarkChurn(b *testing.B, step time.Duration) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(prev) })
	handler, clock := limitedMiddleware(auth.FailureLimits{Burst: 20, Refill: 3 * time.Second, MaxClients: 10_000})
	var worst time.Duration
	for i := 0; b.Loop(); i++ {
		clock.advance(step)
		start := time.Now()
		serveBearer(handler, churnAddr(i), "garbage")
		worst = max(worst, time.Since(start))
	}
	b.ReportMetric(float64(worst.Microseconds()), "worst-µs")
}
